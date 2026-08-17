# Arctic Express — 高值货物舱位与应急改港协同平台

围绕中欧北极快航常态化运营后温控舱位与泊位资源紧张的场景，为货代订舱专员、宁波穿山港调度员、冷链仓管员、船东航线运营、报关行提供的一个后端协同平台。

服务默认监听 **58021** 端口，使用 Go 1.26 编写，仅依赖标准库，状态保存在进程内存中，并以后台任务执行 72 小时未缴订金自动释放与温控柜 10 分钟越界备用柜调拨。

## 业务规则

- **规则一** 温控舱位仅对通过「动力电池类目资质审核」的货代开放。
- **规则二** 超售上限 3%，开港前 72 小时自动释放未缴订金舱位。
- **规则三** 温控柜温度连续 10 分钟越界即派发备用柜调拨单并冻结原柜装船。
- **规则四** 苏伊士爆仓改港北极快航须航线运营二次审批且不晚于截关前 48 小时。
- **规则五** 舱位锁定、改港、退订互斥，任一操作同一时刻仅一个角色持编辑锁。

并发与失败恢复：两船抢同一泊位或两专员抢末个舱位时按最早提交时间戳判定归属，落败方入候补队列并实时通知；改港与报关并行引发舱单版本冲突时，回滚至最近已确认舱单并冻结装船，待航线运营与报关行双方确认后解除冻结并重算截止时间。

## 目录结构

```
cmd/server/main.go            程序入口，监听 :58021 并启动后台任务
internal/domain/              领域模型、状态机、值对象（entities/booking/container/manifest/portchange/errors）
internal/store/               进程内存持久化与全局互斥
internal/service/             应用编排：booking/route/warehouse/dispatch/customs/lock
internal/worker/              后台任务：订金释放、温控越界监控
internal/transport/           HTTP 接入层（net/http 路由）
```

- 生产包 6 个（domain / store / service / worker / transport / cmd），生产 Go 文件 17 个。
- 测试文件 7 个，有效测试函数 25 个，覆盖正常路径、错误路径、状态迁移、并发抢占与失败恢复。

## 主要接口

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET  | `/api/health` | 健康检查 |
| POST | `/api/forwarders` | 注册货代 |
| POST | `/api/forwarders/{id}/qualifications` | 授予类目资质（规则一） |
| POST | `/api/vessels` `/api/ports` `/api/berths` | 维护船舶/港口/泊位 |
| POST | `/api/voyages` | 创建航次（按费利克斯托→鹿特丹→汉堡→格丁尼亚顺序） |
| GET  | `/api/voyages/{id}` | 航次余量 |
| POST | `/api/bookings` | 创建订舱（抢占温控舱位） |
| POST | `/api/bookings/{id}/deposit` | 缴订金 |
| POST | `/api/bookings/{id}/confirm` | 锁定舱位（规则五编辑锁） |
| POST | `/api/bookings/{id}/cancel` | 退订（退订与改港、舱位锁定互斥） |
| POST | `/api/berths/{id}/allocate` | 抢占泊位（最早时间戳判定） |
| POST | `/api/containers` | 绑定温控柜 |
| POST | `/api/containers/{id}/readings` | 记录温度曲线 |
| POST | `/api/containers/{id}/transfer` | 派发备用柜调拨（规则三） |
| POST | `/api/portchanges` | 申请改港（规则四） |
| POST | `/api/portchanges/{id}/approve` `/apply` | 航线运营二次审批并应用 |
| POST | `/api/manifests/{voyageId}/prepare` `/declare` `/confirm` | 舱单准备、报关、冲突恢复双确认 |
| POST | `/api/locks` | 显式获取/释放编辑锁 |
| GET  | `/api/notifications?role=...` | 实时通知（候补/改港/冲突） |

## 启动方式

```bash
go run ./cmd/server
# 或
go build -o arcticexpress ./cmd/server && ./arcticexpress
```

服务监听 `http://localhost:58021`。快速验证：

```bash
curl -s http://localhost:58021/api/health
```

## 测试方法

```bash
go test -timeout=120s -count=1 ./...
```

## Docker

`Dockerfile` 为多阶段构建：`golang:1.26` 构建静态二进制，`scratch` 运行镜像只保留可执行文件并 `EXPOSE 58021`，支持 amd64 与 arm64。

```bash
# 当前架构构建并运行
docker build -t arcticexpress .
docker run --rm -p 58021:58021 arcticexpress

# 多架构构建（amd64 + arm64）
docker buildx build --platform linux/amd64,linux/arm64 -t arcticexpress .
```

运行后访问 `http://localhost:58021/api/health`。
