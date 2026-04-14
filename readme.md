# TEE 管理平台工作流程 (TEE Management Platform)

![Gemini_Generated_Image_je7vc6je7vc6je7v](https://cdn.jsdelivr.net/gh/xtcamille/TyporaImages//img/20260407160226831.png)

TEE Management Platform 是一款基于 **HyperEnclave + Occlum** 硬件可信执行环境（TEE），打通代码提供方与数据提供方的底层信任枢纽。平台支持**并发多租户执行**，确保每次运行的内存安全隔离，提供高度可视化的节点状态侦测，并支持环境阅后即焚与极速底层热重启。

## 核心流程说明

1. **编译打包**：将数据处理逻辑（`enclave-app`）跨平台编译为 PIE 二进制，并打包为 `enclave.tar.gz`。
2. **代码上传 (提供方)**：代码连接器 (`code.html` 前端界面) 将隔离压缩包传入平台，并获得一个全局唯一的 **TaskID**。
3. **隔离启动 (平台)**：TEE 平台基于 `TaskID` 分配独立沙箱与网络环境（动态分配监听端口），在硬件加密区内拉起业务服务。
4. **实时追踪 (前端可视化)**：前端 Dashboard (`index.html`) 实时追踪多任务节点处于 `CODE_UPLOADED`、`ENCLAVE_RUNNING` 等各个阶段的状态与事件流。
5. **代理中转 (无视跨域与自签拦截)**：数据连接器作为常驻服务的 HTTP Proxy 运行（8082端口）。通过前端界面输入 `TaskID` 与业务数据，数据会被直接打包发往本地安全代理服务。
6. **安全传输 (数据方)**：本地代理自动向平台调度中心索要最新分配的动态物理端口，并经强制 **RA-TLS 层** 双向硬件级加密，将业务流数据直接打入隔离内部深处。
7. **自动化清理回收 (隔离区)**：全过程在绝对安全的高性能内存区解析并计算。运算完毕后将结果沿安全信道返回给控制面板；同时，主进程侦测倒计时并强行卸载销毁物理磁盘上产生的几百兆临时沙盒文件夹（`/tmp/occlum_workspace_xxx`），实现环境占用零残留。

## 架构体系拓扑

```mermaid
graph TD
    %% 定义参与方
    subgraph Frontend [一体化管理前台]
        UI[Enclave Dashboard<br/>可视化控制面板]
    end

    subgraph TEE_Platform [TEE 管理平台 (Host)]
        B{Manager Service<br/>并发任务调度 & 垃圾回收器}
        C[[Occlum Enclave Worker 1]]
        C2[[Occlum Enclave Worker N]]
    end

    subgraph Data_Owner [数据提供方/代理]
        D[Data Connector Proxy<br/>HTTP 表单 -> RA-TLS 桥接]
    end

    %% 流程连接
    UI -- "1.传入代码 (tar.gz)" --> B
    B -- "2.热重启/分配沙盒与动态端口" --> C
    B -.-> C2
    C -- "3.生命周期事件监听触发物理销毁" --> B
    UI -- "4.经由页面表单提交数据 + TaskID" --> D
    D -- "5.动态拉取寻址 & RA-TLS 注入核心数据" --> C
    C -- "6.完全在内存层面计算并安全返回脱敏结果" --> D

    %% 样式美化
    style UI fill:#f96,stroke:#333,stroke-width:2px
    style B fill:#69f,stroke:#333,stroke-width:2px
    style C fill:#5fb,stroke:#333,stroke-width:4px
    style C2 fill:#5fb,stroke:#333,stroke-width:2px,stroke-dasharray: 5 5
    style D fill:#f96,stroke:#333,stroke-width:2px
```

## 平台一键极速部署手册

基于最新重构结构，平台现已跨越手动命令行的阶段，支持全自动化一键体系群控。

### 1. (Windows) 使用 BAT 批处理一键全环境拉起
在主目录双击运行 `start-clients.bat`。它会自动将三个核心挂载进程弹窗式启动：
- Manager API 中枢层 (8081端口)
- Dashboard 数据分析可视化前端 (5174端口)
- Data Proxy 通信代理防拦截中转服务 (8082端口)

### 2. (Linux/WSL) 使用 Bash 脚本守护后台启动
```bash
chmod +x start-all.sh
./start-all.sh
```
所有三核服务将被同时丢入 Bash 守护控制后台运行，并在同一个终端交织实时打印。测试完毕想关闭释放端口时，**仅需要随时在键盘按下 `Ctrl+C`**，其内置挂载的 `trap` 雷达会自动强行群控结束并扫清这 3 个 Go 微服务进程！

### 3. 操作与使用演示流

1. 浏览器打开 `http://127.0.0.1:5174` 直接登陆进入 **Trusted Execution Console** 看板。
2. 点击 **Step 1: Code Connector** 面板上传您的 `enclave-app` 原始安全核压缩包并一键点火启动。底层 Manager 会在其宿主机自动完成组装开荒。
3. 从主概览控制台，复制刚刚分配的全局唯一 `Task ID`。若任务因故意外崩溃或者计算结算完毕自动退出，您完全可以通过选中这个 Task 对应的区块，直接点击新增加的“**重新启动**”按钮实现基于母本纯净二进制包的超速热重载。
4. 转入 **Step 2: Data Connector** 面板。过去被恶心和劝退的 `您的连接不是私密连接/跨域拦截` 警告被**彻底消灭**！您只需粘贴刚刚从前一页复制出来的此项 TaskID，并选择本地机器里的机密 `students.csv` 数据即可点击发送。底层常驻挂载监听的 `http://127.0.0.1:8082` HTTP代理会光速接管全部复杂的 RA-TLS 证书防伪、握手建连等操作，您只需坐等界面弹出最终安全的解密分析报告！
