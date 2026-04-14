# Trusted Execution Console (Frontend)

`enclave-manager-frontend` 是一个零 Node 依赖的极小型前台子项目。它基于原生 Vanilla HTML/CSS/JS 以及一个超轻量级的 Go 静态宿主器，承载了对整个 `enclave-manager` 及底层硬件调度的高级可视化交互编排。

经过重构升级，前端目前蜕变成为了一套**专业的多功能场景分离控制台**：

- **`index.html` (总览看板 Dashboard)**: 通过内置 Go 代理安全且无跨域负担地轮询 `/api/task-status`，实时聚合跟踪所有上游租户的 TaskID 与运行状态。包含环境热启动、物理一键强切重载等重功能操作。
- **`code.html` (代码连接器模拟 UI)**: 提供了将最初始干净的宿主安全核应用（`enclave.tar.gz`）一键推送到加密主系统的专业交互切面。
- **`data.html` (数据连接器模拟 UI)**: 借助后台专属 Proxy 服务彻底消灭了浏览器因为自签名证书导致的安全警告和直连阻断！允许用户在界面直接绑定某个正在计算环境里潜伏的 `TaskID` 并把机密文件喂进去，秒级获取最终解析结果。

## 运行方式 (全域启动)

为了体验最丝滑的操作流，**强烈推荐使用项目根目录下提供的一键脚本**进行三核并发拉起：

- **Windows 系统**: 双击根目录下的 `start-clients.bat`
- **Linux/Mac/WSL 系统**: 执行根目录的 `./start-all.sh`

*(当然，您也可以单独做 UI 本地修改和调试：)*

```powershell
cd d:\zxt\tee-management-platform\enclave-manager-frontend
$env:MANAGER_BASE_URL="http://192.168.0.248:8081"
go run .
```

默认访问统一控制台地址：

```text
http://127.0.0.1:5174
```

## 可选环境变量

- `MANAGER_BASE_URL`: 后端 `enclave-manager` 任务调度中枢层地址，当前硬编码缺省为指向我们内网的高性能物理节点 `http://192.168.0.248:8081`（如果在本地测试则按需设置为 `http://127.0.0.1:8081`）。
- `PORT`: 本专属前端承载的 Web 服务器监听端口，默认 `5174`。

## 极致的前后端边界互动体验

- **彻底突破浏览器证书阻断**: 由于现代浏览器底层安全机制暴力封锁了未经过官方机构签名的本地 `RA-TLS` 握手。现在，`data.html` 提交的机密表单目前会极其机智地静默发给本地本机的 `data-connector` 后台层 (Port `8082`)，完成内部代理硬核握手加密，因此在控制台能畅通无阻直接打印回传级结果！
- **完美跨域管理**: `/api/*` 请求全部被本模块中的 `main.go` 反向代理吞没接管，所以完全不需要再往 `enclave-manager` 代码里加上乱七八糟的 CORS 修改。
- **环境隔离阅后即焚**: 得益于前后端的极致解耦，您能在本看板上一键控制销毁重组动辄几百兆开销的 Occlum 执行期沙盒文件。
