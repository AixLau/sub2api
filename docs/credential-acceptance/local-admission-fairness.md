# 本地准入隔离与有界公平

`report_revision=local-admission-fairness-r1`，工作起点 `44f88b3d55c170205b1e6d120895f8691132bb3a`。用户最后限定为**仅本机测试验收**：原生Linux/amd64复测不再作为本轮执行项。本机Darwin/arm64、OrbStack/aarch64、PostgreSQL镜像linux/amd64且实际18.4；不能称为原生或独占硬件SLO。生产主机仅做过只读架构、负载检查，无测试部署、服务中断、配置变更或生产数据访问。

20ms严格指规格21.2的**权威admission事务p95**。本地调度等待、跨节点try-lock竞争、call/请求到获准时间另列，并对实际handler使用15秒预算验收。E3已通过的生命周期保护、可靠receipt恢复是回归约束；不再统称“生命周期饥饿仍未解决”。没有放宽上限、延長期限或删除权威检查。

## 静态发现、复现和修复

| 问题 | 基线实际复现 | 实现与证据 |
|---|---|---|
| A跨节点锁竞争占住整个store的本地gate，阻塞B | A已实际rollback至少两次、其外部锁保持时，B的750ms期限耗尽 | `ec091df81`按主体保留本地队列，跨节点失败后让出节点turn再重试；B约315.58ms获准；节点仍最多一个准入/推进事务。详见 [主体隔离](local-principal-isolation.md) |
| offered/pump无限优先 | 第9个高优先仍取得grant，fresh无机会 | `4c0c3f8fe`：每类FIFO，队头fresh最多被8个已完成的高优先turn越过。界限按turn计，不承诺被外部数据库阻塞时的墙钟时限 |
| 已取消等待者仍占256项本地名额 | 256个acquire均返回取消，切片仍满；新请求误报STORE_UNAVAILABLE | 同提交及时移除取消者；grant/cancel在同mutex下只转交一次；新`ErrAdmissionLocalQueueFull`映射独立REJECTED/503 |
| handler确认晚于15秒后发送／未登记超时误报存储错误 | 实际mock收到late ACK/lost ACK请求；未登记3请求到期仍报STORE_UNAVAILABLE | `b53f2b7d8`返回或恢复同RESERVED后重查期限，以原3秒补偿预算撤销；见 [handler 15秒证据](handler-queue-budget.md) |

registry对所有主体合计最多257个有效调用，含一个运行者和256个等待者；不会将等待容量按主体倍增。清理最后引用后回收主体条目；取消context可立即返还计数，但不提前拆分仍被旧调用引用的主体队列。queue pump按主体轮流取得节点turn，单次失败不阻断其他主体。没有schema迁移、连接预算或业务功能增加。

## 已运行定向检查

所有Go命令在`backend/`，使用`GOCACHE=/tmp/sub2api-queue-progress/go-cache`；integration另设`TESTCONTAINERS_RYUK_DISABLED=true CI=true`，均保持vet开启。

- gate基线3项FAIL；修复`go test -p 2 ./internal/repository -run '^TestCredentialAdmissionTurn' -count=1 -timeout=60s -v` PASS；`-race -count=10` PASS，含1000次grant/cancel交错。原始日志 `/tmp/sub2api-local-admission-fairness/{gate-baseline,gate-fixed,gate-race}.log`。
- 主体隔离基线FAIL；修复`go test -p 2 -tags=integration ./internal/repository -run '^(TestCredentialPrincipalTurn|TestCredentialQueueProgress|TestCredentialLifecycleResources|TestPrincipalAdmissionThreeNodesLastSlot)' -count=1 -v` PASS；registry单测和race各20轮PASS。日志 `/tmp/sub2api-local-admission-isolation/`。
- handler 15秒实际突发、取消、late ACK及本地满错误码集成PASS（76.194秒）；本地满通过wrapper注入decision验证HTTP映射，不冒称该用例触发了真实256限额。日志 `/tmp/sub2api-local-admission/handler/after-stable.log`。
- 计时器自身增加真实PG验证`TestCredentialAdmissionTraceCountsRetriesAndCommit`：一个advisory失败回滚、一个成功提交，分别计数且累计事务时间包含两次尝试；PASS（8.316秒），`/tmp/sub2api-local-admission/root/trace-verify.log`。

## 单主体成本测量

本轮保留原C+3 worker、3节点pool各6+2总24连接、2分钟request/context、100ms批量提示轮询、每分钟C+3 barrier、原200ms/2s/5s/12s mock分布、Heartbeat10s/3s及Finish5s。只更新测量：

- 每次try-lock实际布尔结果（成功/失败）计数，不用SQL文本出现次数推断获锁。
- 最后一次权威事务时长与全部重试事务累计时长分开；每次重新Begin清除上次锁持有起点，记录成功/回滚/错误计数。
- 主体锁取得后到事务结束的平均持有时间下界、事务平均时间、有效成功准入/秒、成功准入/try-lock尝试比率。
- 所有事务的平均时长单列，**其中包含短失败重试、Heartbeat、Finish等，不能冒称成功准入事务均值**。
- 未带请求trace的后台SQL累计wall时间/次数（queue/control）单列，线程重叠时不能将和解释成墙钟CPU占比。
- 本地待调用数读取主体registry，不能只读节点gate漏掉等主体turn的请求。采样估计并非准确空闲墙钟，更不能外推真实权限/健康变化时所有请求可执行。

30秒C200/I16诊断命令：

```sh
GOCACHE=/tmp/sub2api-queue-progress/go-cache TESTCONTAINERS_RYUK_DISABLED=true CI=true \
SUB2API_CREDENTIAL_ENDURANCE=30s SUB2API_CREDENTIAL_MATRIX_CASE=C200_I16 \
go test -p 2 -tags=integration ./internal/repository -run '^TestCredentialAcceptanceEndurance$' -count=1 -timeout=4m -v
```

实际PASS；1647次dispatch、37.14/s，无ERROR，最终全部RELEASED。成功准入事务平均19.79ms、p95 28.49ms，主体持锁下界平均9.255ms；call p95 5601.61ms，前置等待p95 5578.30ms。共有12241次advisory尝试，1647次获锁、10594次失败回滚，有效准入/尝试13.45%；没有WAIT或后台推进样本，不能据此判断有竞争票据时的成本。它显示单主体仍有大量跨节点无效重试，隔离修复没有天然提高单主体吞吐。没有用p95倒数推断上限。日志`/tmp/sub2api-local-admission/root/single-principal-cost.log`。

## 本机固定矩阵

待本轮固定六组合各10分钟结果；受测代码SHA、发生器SHA、日志hash与报告revision分别记录。不重跑与修改无关的全套测试或全量编译。

## 迁移、回滚和未验证

本轮无DB迁移；保留既有259 offer列及生命周期保留池、receipt恢复协议。回滚前保持功能关闭，暂停grouped新请求并按原证据完成已知终结工作；不清空账本、UNKNOWN或旧绑定，不恢复旧token快照。回退相应代码会重新引入已复现的隔离/饥饿或late ACK问题，因此不能作为生产启用方案。

原生Linux/amd64、独占宿主性能、三个完整网关进程的发布演练仍未验证；本轮按最后指令仅本机。真实provider身份与compact继续BLOCKED，导入UNVERIFIED；功能默认关闭。权限和未知执行边界未放宽，WS、session/full、UA/TLS、隐私清理及原未跟踪文件均未修改。
