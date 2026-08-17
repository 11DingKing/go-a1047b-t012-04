# Bug Reproduction

## 包的性质

当前 test_model_fix 保存的是被测模型修复后的结果源码，不是初始含 Bug 源码。要复现原始缺陷，必须检出下面固定的 parent SHA；不要在当前修复结果源码上期待重新出现修复前失败。生成系统使用的可信验证补丁和完整验证日志仅在本地留存，不提交到结果分支。

## 问题现象

帮我查一个订舱锁定舱位之后就再也改不动的问题。先不要改代码，我需要先拿到准确的根因和证据再决定怎么动。

现象：

1. 一笔订舱缴完订金、锁定舱位成功之后，这笔订舱的编辑锁一直显示被「订舱专员」持有，锁列表里也一直挂着这条锁记录，没有任何人再去动它。
2. 苏伊士爆仓要改港时，航线运营对这笔已锁定舱位的订舱发起改港申请，直接被编辑锁挡住，提示这笔订舱被订舱专员持有。改港这条路整个走不通，只能等——但等多久都没用，锁不会自己掉。
3. 有个很怪的规律：订舱专员自己的操作反而都正常。同一笔订舱锁定完之后再退订，退订能成功、状态变 cancelled，而且退订之后锁反而干净了。
4. 从没锁定过舱位的订舱（只缴了订金）改港是正常的，不会被拦。
5. 规则五本身的互斥没坏：航线运营先占着这笔订舱时，订舱专员去锁定舱位会被正确拒绝；对方释放后再锁定就能成功。
6. 服务重启会清空进程内状态，但只要重新走一遍锁定舱位，这个锁就又卡住了。

复现：建一个航次和一个有资质的货代，下一笔温控订舱、缴订金、锁定舱位，然后查这笔订舱的编辑锁持有者；再让航线运营对同一笔订舱发起改港。

请定位这个「锁定舱位之后订舱编辑锁交不出去」的根因：说明是哪个 Go 文件里的哪个符号、它的什么错误行为，以及这个错误行为为什么会造成上面这些症状（包括为什么退订和同角色再锁定不受影响、为什么从没锁定过的订舱改港正常）。先给结论和证据，不要改仓库里的代码。

## 含 Bug 版本

- 仓库：11DingKing/go-a1047b-t012-04
- 仓库地址：https://github.com/11DingKing/go-a1047b-t012-04.git
- parent SHA：4aba960cac3791b478724fd680b14351963741f4

## 复现步骤

```bash
git clone -- https://github.com/11DingKing/go-a1047b-t012-04.git bug-repro
cd bug-repro
git checkout --detach 4aba960cac3791b478724fd680b14351963741f4
go test -timeout=120s ./internal/service/ -run "^(TestBookingEditLockIsFreeAfterBerthLock|TestPortChangeWorksAfterBerthLock|TestSecondBerthLockAndRefundStillWorkAfterBerthLock|TestPortChangeWorksOnBookingThatWasNeverLocked|TestBerthLockStillHonoursAnotherRolesLock)$" -count=1 -v
```

## 双架构完整错误信息

### linux/amd64

- 容器内复现预期退出码：1
- 容器内复现实际退出码：1

stdout：

```text
$ go test -timeout=120s ./internal/service/ -run "^(TestBookingEditLockIsFreeAfterBerthLock|TestPortChangeWorksAfterBerthLock|TestSecondBerthLockAndRefundStillWorkAfterBerthLock|TestPortChangeWorksOnBookingThatWasNeverLocked|TestBerthLockStillHonoursAnotherRolesLock)$" -count=1 -v
=== RUN   TestBookingEditLockIsFreeAfterBerthLock
    booking_handover_test.go:30: booking edit lock still held by "booking_specialist" after the berth was locked, want it released
--- FAIL: TestBookingEditLockIsFreeAfterBerthLock (0.00s)
=== RUN   TestPortChangeWorksAfterBerthLock
    booking_handover_test.go:48: route ops must be able to request a port change on a locked booking, got edit lock held by another role: booking:BKG-1 held by booking_specialist
--- FAIL: TestPortChangeWorksAfterBerthLock (0.00s)
=== RUN   TestSecondBerthLockAndRefundStillWorkAfterBerthLock
--- PASS: TestSecondBerthLockAndRefundStillWorkAfterBerthLock (0.00s)
=== RUN   TestPortChangeWorksOnBookingThatWasNeverLocked
--- PASS: TestPortChangeWorksOnBookingThatWasNeverLocked (0.00s)
=== RUN   TestBerthLockStillHonoursAnotherRolesLock
--- PASS: TestBerthLockStillHonoursAnotherRolesLock (0.00s)
FAIL
FAIL	arcticexpress/internal/service	0.029s
FAIL

```

stderr：

```text
(empty)
```

### linux/arm64

- 容器内复现预期退出码：1
- 容器内复现实际退出码：1

stdout：

```text
$ go test -timeout=120s ./internal/service/ -run "^(TestBookingEditLockIsFreeAfterBerthLock|TestPortChangeWorksAfterBerthLock|TestSecondBerthLockAndRefundStillWorkAfterBerthLock|TestPortChangeWorksOnBookingThatWasNeverLocked|TestBerthLockStillHonoursAnotherRolesLock)$" -count=1 -v
=== RUN   TestBookingEditLockIsFreeAfterBerthLock
    booking_handover_test.go:30: booking edit lock still held by "booking_specialist" after the berth was locked, want it released
--- FAIL: TestBookingEditLockIsFreeAfterBerthLock (0.00s)
=== RUN   TestPortChangeWorksAfterBerthLock
    booking_handover_test.go:48: route ops must be able to request a port change on a locked booking, got edit lock held by another role: booking:BKG-1 held by booking_specialist
--- FAIL: TestPortChangeWorksAfterBerthLock (0.00s)
=== RUN   TestSecondBerthLockAndRefundStillWorkAfterBerthLock
--- PASS: TestSecondBerthLockAndRefundStillWorkAfterBerthLock (0.00s)
=== RUN   TestPortChangeWorksOnBookingThatWasNeverLocked
--- PASS: TestPortChangeWorksOnBookingThatWasNeverLocked (0.00s)
=== RUN   TestBerthLockStillHonoursAnotherRolesLock
--- PASS: TestBerthLockStillHonoursAnotherRolesLock (0.00s)
FAIL
FAIL	arcticexpress/internal/service	0.002s
FAIL

```

stderr：

```text
(empty)
```

## 通过条件

通过标准（diagnosis）：
1. 命中 gold 根因涉及的文件：internal/service/booking_service.go
2. 命中 gold 根因涉及的符号：(*BookingService).ConfirmBooking
3. 命中正确的失效机制：该方法用 acquireLock 取得按订舱的规则五编辑锁后，缺少 defer bs.releaseLock(lockKeyBooking(bookingID)) 这一步，函数正常返回时锁没有被交还，于是 Store.Locks 里一直留着 Holder=booking_specialist 的记录；后续 RouteService.RequestPortChange 以 RoleRouteOps 去 acquireLock 时因持有者不同而返回包装了 ErrLockHeld 的错误，改港被永久挡住。还需说明 acquireLock 对同一持有者是幂等刷新（因此订舱专员自己的退订与再次锁定不受影响），以及 CancelBooking / RequestPortChange 都写了 defer 释放（因此退订之后锁反而变干净、从没锁定过的订舱改港正常）
4. 结论有实际证据（读过相关代码或跑过复现），不是凭空推断
5. 目标仓库全程零改动；容器内一次性独立复现程序不计为项目代码改动
6. 复现依据：
   go test -timeout=120s ./internal/service/ -run "^(TestBookingEditLockIsFreeAfterBerthLock|TestPortChangeWorksAfterBerthLock|TestSecondBerthLockAndRefundStillWorkAfterBerthLock|TestPortChangeWorksOnBookingThatWasNeverLocked|TestBerthLockStillHonoursAnotherRolesLock)$" -count=1 -v
   在 main 上失败、在 gold_model_fix 上通过
