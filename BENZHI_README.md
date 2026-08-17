# BENZHI_README

## 项目说明

- 项目：11DingKing/go-a1047b-t012-04
- 项目用途：围绕中欧北极快航常态化运营后温控舱位与泊位资源紧张的场景，为货代订舱专员、宁波穿山港调度员、冷链仓管员、船东航线运营、报关行提供的一个后端协同平台。
- Go 工具链：`golang:1.26`
- 前端工具链：无

## 标准构建、运行和测试命令

进入容器后执行：

```bash
# 编译
cd '/app' && GOTOOLCHAIN=local go build ./...

# 启动
cd '/app' && GOTOOLCHAIN=local go run ./cmd/server

# 测试
cd '/app' && GOTOOLCHAIN=local go test ./...
```

## Docker 构建和进入容器

```bash
chmod +x build_benzhi_docker.sh
./build_benzhi_docker.sh benzhi-task-54-amd64 linux/amd64
./build_benzhi_docker.sh benzhi-task-54-arm64 linux/arm64
docker run -it benzhi-task-54-amd64:latest
docker run -it --platform linux/arm64 benzhi-task-54-arm64:latest
```

## 题目验证命令

1. 预期退出码 1：`go test -timeout=120s ./internal/service/ -run "^(TestBookingEditLockIsFreeAfterBerthLock|TestPortChangeWorksAfterBerthLock|TestSecondBerthLockAndRefundStillWorkAfterBerthLock|TestPortChangeWorksOnBookingThatWasNeverLocked|TestBerthLockStillHonoursAnotherRolesLock)$" -count=1 -v`

## Bug 复现

Bug 现象、触发步骤和完整错误信息见 `BUG_REPRO.md`。
