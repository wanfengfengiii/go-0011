# 施工混凝土试件龄期监管与强度封存服务

## 项目目标

构建一个面向大型施工现场的 Go HTTP 服务，以追加式事件记录为事实来源，管理浇筑区段、混凝土配合比、取样组、试件和龄期检验规则。服务并发接收取样、制模、脱模、养护环境、转运和压力试验事件，按事件时间在固定水位线内确定性重排，并通过试件版本和组级状态机生成完整监管链。压力试验完成后冻结评定输入，计算等效龄期、抗压强度和组级结论；满足限定条件时可发起一次复核，最终结论仅能封存一次。SQLite 事务存储事件、幂等键、快照和封存摘要，进程重启后可回放并验证恢复结果。代码规模规划为约 24—28 个生产 Go 文件、约 2500 行有效生产代码，控制在 2000—3000 行，分布于 cmd/server、internal/domain、internal/ingest、internal/calculation、internal/evaluation、internal/storage 和 internal/httpapi 等有业务职责的包；公开测试不少于 5 个测试文件和 14 个确定性用例。仓库包含 go.mod、迁移文件、示例 JSON 事件脚本，以及可在 linux/amd64 与 linux/arm64 构建的多阶段 Dockerfile。

## 端到端业务流程

1. 检验建档：质量人员依次登记工程、浇筑区段、配合比和龄期规则，再创建取样组及其试件编号；创建时冻结设计强度、目标龄期、试件尺寸、组内数量、允许温度范围和评定阈值，组状态为“待取样”。
2. 现场事件接入：采集设备或人员按统一 JSON 信封提交事件；服务先校验来源、试件编号、来源序列号、事件时间、单位和期望版本，再在一个事务内写入事件和幂等结果。相同幂等键与相同载荷返回原结果，相同键但载荷不同返回身份冲突。
3. 时序归并：每个试件维护最大已见事件时间，以其减去固定 10 分钟作为水位线；水位线之后的事件保留在待排序区，水位线之前的事件按事件时间、事件类型优先级、来源和序列号稳定排序后应用。早于已封闭水位线的迟到事件被分类拒绝，不改变状态。
4. 养护监管：取样和制模后进入“养护中”，环境温度事件按时间段积分生成等效龄期；脱模、养护切换和转运会切分环境区间。达到目标等效龄期且所有必要试件已到达试验地点后，组进入“待试验”；缺测或越界可能把试件标记为无效。
5. 压力试验：客户端从“待试验”状态声明某个试件开始试验，组进入“试验中”；服务校验压力机、载荷曲线、峰值载荷、单位、尺寸和版本，并在同一事务中记录试验事件及计算结果。最后一个必要试件完成后冻结组级评定快照并进入“待复核”。
6. 评定与复核：服务仅使用冻结快照计算平均强度、最低强度及有效试件数量。无争议时直接封存为“合格”“不合格”或“无效”；仅当存在可纠正的设备校准、尺寸录入或试件身份争议且尚未封存时，允许一次复核，复核通过追加纠正证据形成关联的新快照，不修改原事件，随后封存最终结论。
7. 崩溃恢复：启动时加载最近检查点并按全局事件位置回放后续记录，重新构建试件版本、待排序事件、组状态、计算值和摘要；若检查点校验不一致则从事件起点完整回放。恢复完成前写接口返回暂不可用，读接口可返回明确的恢复状态。

## 核心组件与职责

1. 工程与检验规则模型：位于 internal/domain，定义工程、浇筑区段、配合比、取样组、试件、龄期规则、单位值对象、事件类型及状态迁移约束；计划 5—6 个生产文件、约 430 行。
2. 试件身份及事件接入：位于 internal/ingest，负责 JSON 信封规范化、幂等键、身份绑定、期望版本比较、固定水位线缓冲和稳定排序；计划 4—5 个生产文件、约 430 行。
3. 养护时序计算器：位于 internal/calculation，切分养护区间，处理温度缺测和越界，按固定换算表累计等效龄期并输出有效性原因；计划 3—4 个生产文件、约 360 行。
4. 压力试验校验器：位于 internal/calculation，校验压力机曲线、峰值载荷、单位和试件尺寸，执行面积及尺寸系数换算，输出强度和设备数据失效原因；计划 3—4 个生产文件、约 340 行。
5. 组级评定状态机：位于 internal/evaluation，聚合试件结果、生成冻结快照、执行初评和一次性复核、封存结论并生成规范摘要；计划 4—5 个生产文件、约 450 行。
6. 事件持久化与恢复：位于 internal/storage，并由 internal/httpapi 和 cmd/server 调用；使用 SQLite 事务保存事件、幂等回执、聚合版本、快照和检查点，支持故障注入、回放及 HTTP 错误映射；计划 6—7 个生产文件、约 500 行。

## 领域规则与不变量

1. 每个取样组只能属于一个浇筑区段和一个配合比；试件编号在工程内唯一，创建后不得转移。组创建时冻结设计强度、目标龄期、试件边长、必要试件数、温度规则、尺寸系数和评定阈值，后续规则修改只影响新组。
2. 事件信封必须包含 source、specimen_id、sequence、occurred_at、expected_version、type 和 payload。幂等键为 source/specimen_id/sequence；重复键且规范化载荷摘要相同返回首次状态码、版本和结果，摘要不同返回 IDENTITY_CONFLICT。
3. 每次成功应用事件使对应试件版本加一。expected_version 必须等于当前版本；同一版本上的并发请求最多一个成功，其余返回 VERSION_CONFLICT。组级写入还需比较组版本，避免不同试件同时完成试验时重复冻结或封存。
4. 固定乱序窗口为 10 分钟。稳定排序键依次为 occurred_at、业务优先级、source、sequence；同一时刻的优先级为取样、制模、环境、脱模、转运、开始试验、载荷曲线、完成试验。已越过水位线且早于最后应用位置的事件返回 LATE_EVENT，不进行追溯改写。
5. 等效龄期按分钟积分。相邻温度点间隔不超过 120 分钟时使用前值保持；超过 120 分钟的超出部分记为缺测。每分钟系数由冻结表确定：低于 0℃为 0，0—5℃为 0.25，5—10℃为 0.50，10—15℃为 0.75，15—25℃为 1.00，25—35℃为 1.20，高于 35℃为 0.80。任一连续缺测超过 6 小时，或连续越出规则允许范围超过 2 小时，试件无效；否则累计分钟除以 1440 得到保留三位小数的等效龄期。
6. 压力载荷只接受 kN，试件边长只接受 mm，强度单位固定为 MPa。峰值强度等于峰值载荷乘 1000 再除以受压面积，并乘冻结尺寸系数：100 mm 为 0.95、150 mm 为 1.00、200 mm 为 1.05，结果按四舍五入保留一位小数。其他尺寸或单位返回 UNIT_OR_DIMENSION_ERROR。
7. 载荷曲线必须包含至少 5 个时间严格递增的点，从首个正载荷到峰值不少于 3 秒；载荷不得为负，峰值前单步下降不得超过峰值的 15%，峰值后必须出现至少一次不低于峰值 10% 的下降，客户端申报峰值与曲线峰值偏差不得超过 1%。违反任一规则返回 ABNORMAL_LOAD_CURVE，且不得形成完成试验事件。
8. 组级初评需要达到冻结的必要试件数且所有参与试件有效。默认阈值为平均强度不低于设计强度的 1.15 倍且最低强度不低于设计强度的 0.95 倍；同时满足为合格，否则为不合格，有效试件不足为无效。计算只读取冻结快照。复核仅能在“待复核”、未封存、存在设备校准/尺寸录入/身份争议之一且未复核时执行；纠正证据必须引用原事件，复核后立即封存，sealed_at、结论和摘要不得再次修改。
9. 合法主状态迁移为待取样→养护中→待试验→试验中→待复核→合格/不合格/无效；试验中可在尚无完成试验事件时因取消操作回到待试验。任何其他迁移返回 ILLEGAL_TRANSITION，且事件、版本、计算值和状态均保持不变。

## 数据模型与持久化

1. Project：id、name、site_code、created_at。PourSection：id、project_id、name、location、planned_pour_at。MixDesign：id、project_id、code、design_strength_mpa、material_revision。
2. InspectionRule：id、project_id、revision、target_equivalent_days、required_specimens、allowed_temperature_min/max、missing_limit_minutes、out_of_range_limit_minutes、dimension_factors、mean_factor、minimum_factor、created_at。
3. SampleGroup：id、pour_section_id、mix_design_id、rule_revision、status、version、sampled_at、frozen_snapshot_id、review_count、sealed_conclusion、sealed_at、sealed_digest。
4. Specimen：id、group_id、specimen_no、bound_identity、nominal_side_mm、version、last_applied_at、max_seen_at、effective_age_minutes、validity、current_location。
5. EventRecord：global_position、event_id、source、specimen_id、sequence、occurred_at、received_at、expected_version、event_type、canonical_payload、payload_digest、applied_status、classified_error。
6. PendingEvent：event_id、specimen_id、sort_time、business_priority、source、sequence，用于水位线内的持久化待排序事件，成功应用或确定拒绝后删除。
7. TemperatureSegment：specimen_id、start_at、end_at、temperature_c、equivalent_minutes、quality_flag、source_event_ids。PressureResult：specimen_id、machine_id、curve_digest、peak_load_kn、side_mm、factor、strength_mpa、validity。
8. EvaluationSnapshot：id、group_id、kind、parent_snapshot_id、group_version、rule_json、specimen_results_json、evidence_refs、calculated_conclusion、canonical_digest、created_at。Checkpoint：global_position、aggregate_digest、snapshot_blob、created_at。

## 公开接口

1. POST /v1/projects、/v1/pour-sections、/v1/mix-designs、/v1/inspection-rules 和 /v1/sample-groups：建立检验资料；创建取样组时传入试件编号列表并返回冻结规则摘要。
2. POST /v1/specimens/{specimenID}/events：接收单个事件信封，成功返回 applied、buffered 或 duplicate、当前版本、水位线和衍生结果；业务错误采用稳定的 code、category、retryable、details JSON 结构。
3. POST /v1/events:batch：按输入顺序校验多个事件，但每个事件独立事务提交并返回逐项结果；接口不承诺跨试件原子性，便于设备断线补传且保持确定重放。
4. POST /v1/sample-groups/{groupID}/watermark:advance：使用请求中的确定时间推进该组各试件水位线，仅供显式业务调度和测试，不读取系统当前时间。
5. POST /v1/sample-groups/{groupID}/evaluate：冻结当前合格输入并执行初评；POST /v1/sample-groups/{groupID}/review：提交争议类型、引用事件和纠正证据；POST /v1/sample-groups/{groupID}/seal：在无复核争议时封存初评。
6. GET /v1/specimens/{specimenID}/chain：按确定顺序返回原始事件、应用状态、版本变化、温度区间、压力换算和错误；GET /v1/sample-groups/{groupID} 返回组状态、快照及封存信息。
7. GET /v1/sample-groups/{groupID}/digest：返回规范 JSON 的 SHA-256 摘要及其全局事件位置；GET /health/ready 返回迁移、恢复阶段和最后恢复位置。
8. 进程配置仅包含监听地址、SQLite 文件路径、检查点间隔和日志级别；业务窗口与换算规则来自已冻结的检验规则，测试通过注入 Clock、Repository 和固定规则构造器控制时间与故障。

## 失败边界

1. 请求解析、字段规范化或单位错误发生在事务前，不写事件；错误分别归类为 VALIDATION、UNIT_OR_DIMENSION_ERROR，并返回不可重试标志。
2. 身份冲突、迟到事件、版本冲突和非法状态迁移均为确定性业务拒绝；可将拒绝摘要记入独立审计记录，但不得增加试件业务版本或改变组状态。VERSION_CONFLICT 标记为可在重新读取版本后重试。
3. 开始试验、载荷曲线、完成试验及压力结果必须在同一 SQLite 事务边界内按事件逐步提交；完成试验事件、计算结果、试件版本和组版本作为一个原子提交，任何存储错误全部回滚，不留下半完成结果。
4. 同组最后多个试件并发完成时，冻结快照使用组版本条件更新；竞争失败者重新读取后只能复用已生成快照，不能创建第二份初评快照或第二次封存。
5. 环境缺测、温度越界和异常载荷曲线属于设备数据失效边界，不等同于存储故障。前两者按冻结窗口形成可追溯的无效结果；异常曲线拒绝完成试验并允许提交新序列号的纠正曲线。
6. 进程可在事件写入前、事务提交前或检查点写入后崩溃。启动恢复以已提交事件为准，忽略不完整检查点；回放后的聚合摘要必须与有效检查点一致，否则从头回放并报告 RECOVERY_DIGEST_MISMATCH。

## 验收标准

1. 相同 source/specimen_id/sequence 和相同载荷无论提交多少次都返回首次结果且仅保存一条业务事件；同键不同载荷稳定返回 IDENTITY_CONFLICT。
2. 两个携带相同 expected_version 的并发试件操作恰有一个成功；失败请求返回 VERSION_CONFLICT，最终版本、事件链和组状态不存在丢失更新。
3. 10 分钟水位线内以不同到达顺序提交相同事件集合，推进相同水位线后产生完全相同的事件次序、等效龄期、状态和摘要；越界迟到事件返回 LATE_EVENT。
4. 缺失取样、试件身份冲突、错误单位、异常载荷曲线及非法状态迁移分别返回稳定可分类错误，且失败前后的业务快照一致。
5. 固定温度脚本准确复现正常养护、短时缺测、连续缺测超 6 小时和连续越界超 2 小时的等效龄期及有效性，所有舍入结果与规则一致。
6. 组级评定只读取冻结快照；复核仅在允许的争议条件下执行一次，原快照保持不变，最终合格、不合格或无效结论及 sealed_digest 只写入一次。
7. 在完成试验事务的各故障注入点均不出现半完成试验；使用同一数据库重启后，状态、版本、待排序事件、等效龄期、强度值、快照和规范摘要与崩溃前已提交状态一致。
8. 仓库包含不少于 20 个生产 Go 文件和 2000—3000 行有效生产 Go 代码、至少 4 个有实际职责的包、go.mod 及 amd64/arm64 Docker 构建支持；公开测试至少分布于 4 个测试文件并包含 12 个以上可重复通过的测试用例。

## 确定性测试场景

1. internal/ingest/idempotency_test.go：同一事件重复提交返回首次回执且仓储仅有一条记录。
2. internal/ingest/idempotency_test.go：同一幂等键携带不同温度载荷时返回 IDENTITY_CONFLICT。
3. internal/ingest/ordering_test.go：转运、脱模和环境事件以多种乱序到达，水位线推进后事件链及摘要相同。
4. internal/ingest/ordering_test.go：早于封闭水位线的迟到转运事件返回 LATE_EVENT 且位置不变。
5. internal/calculation/curing_test.go：恒定 20℃正常养护达到目标等效龄期并进入待试验。
6. internal/calculation/curing_test.go：温度短时缺测只扣除超出前值保持窗口的分钟数。
7. internal/calculation/curing_test.go：连续缺测超过 6 小时和连续温度越界超过 2 小时分别形成确定的无效原因。
8. internal/calculation/pressure_test.go：固定 150 mm 试件和峰值载荷换算为预期 MPa，并执行一位小数舍入。
9. internal/calculation/pressure_test.go：错误单位、未知尺寸、申报峰值偏差和异常回落曲线分别被拒绝。
10. internal/evaluation/state_machine_test.go：缺失取样时提交制模事件返回 MISSING_SAMPLING，非法跨状态试验不产生版本变化。
11. internal/evaluation/state_machine_test.go：同组两个试件并发完成试验，只生成一个冻结快照并进入待复核。
12. internal/evaluation/state_machine_test.go：不合格初评因有效设备校准证据复核为合格，原快照不变且第二次复核被拒绝。
13. internal/evaluation/state_machine_test.go：身份混淆导致有效试件不足，组封存为无效且摘要不可再次写入。
14. internal/storage/recovery_test.go：在完成试验事务提交前注入故障，重启后不存在完成事件或压力结果。
15. internal/storage/recovery_test.go：提交后、检查点前崩溃，通过事件回放恢复相同状态、计算值、待排序事件和摘要。
16. internal/httpapi/script_test.go：使用假时钟、内存仓储和固定规则执行 JSON 闭环脚本，覆盖建档、取样、养护、乱序转运、并发试验、评定、复核与封存。

## 组件追踪关系

1. 工程与检验规则模型支撑建档流程、冻结规则、单位约束和合法主状态，覆盖验收 4、5、6。
2. 试件身份及事件接入支撑幂等、乐观版本和水位线重排，覆盖验收 1、2、3，并由 idempotency_test.go 与 ordering_test.go 验证。
3. 养护时序计算器支撑环境区间积分、缺测和越界失效，覆盖验收 5，并由 curing_test.go 的正常、缺测和失控用例验证。
4. 压力试验校验器支撑曲线有效性、载荷面积换算和设备数据边界，覆盖验收 4、7，并由 pressure_test.go 和故障恢复用例验证。
5. 组级评定状态机支撑冻结快照、并发完成、一次复核和一次封存，覆盖验收 2、6，并由 state_machine_test.go 验证。
6. 事件持久化与恢复支撑事务原子性、检查点、事件回放、HTTP 分类错误和确定摘要，覆盖验收 7、8，并由 recovery_test.go 与 script_test.go 验证。

## 独特性

项目以施工混凝土试件为核心，把养护温度时序、压力机载荷曲线、试件身份链、组级冻结评定和崩溃回放收束为一条监管闭环；其固定水位线、等效龄期积分、尺寸换算、一次复核及一次封存规则形成区别于通用事件平台或普通质检台账的明确业务特征。
