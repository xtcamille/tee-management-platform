# TEE 管理平台工作流程 (TEE Management Platform)

![Gemini_Generated_Image_je7vc6je7vc6je7v](https://cdn.jsdelivr.net/gh/xtcamille/TyporaImages//img/20260407160226831.png)

TEE Management Platform 是一款基于 **HyperEnclave + Occlum** 硬件可信执行环境（TEE），打通代码提供方与数据提供方的底层信任枢纽。平台支持**并发多租户执行**，确保每次运行的内存安全隔离，并提供高度可视化的节点状态侦测。

## 核心流程说明

1. **编译打包**：将数据处理逻辑（`enclave-app`）跨平台编译为 PIE 二进制，并打包为 `enclave.tar.gz`。
2. **代码上传 (提供方)**：代码连接器 (`code-connector`) 将隔离压缩包传入平台，并获得一个全局唯一的 **TaskID**。
3. **隔离启动 (平台)**：TEE 平台基于 `TaskID` 分配独立沙箱与网络环境（动态分配监听端口），在硬件加密区内拉起业务服务。
4. **实时追踪 (平台)**：可通过 `/task-status` 并发级监控任务处于 `CODE_UPLOADED`、`ENCLAVE_RUNNING` 或 `DATA_RECEIVED` 等阶段。
5. **安全传输 (数据方)**：数据连接器 (`data-connector`) 通过分配的端口，经强制 **RA-TLS 层** 加密直接将流数据注入到隔离环境中。
6. **就地计算 (隔离区)**：在绝对安全的高性能内存区进行数据解析与运算，结果仅通过安全信道返回给 Data Connector。

## 架构体系拓扑

```mermaid
graph TD
    %% 定义参与方
    subgraph Dev [代码提供方]
        A[Code Connector<br/>代码连接器]
    end

    subgraph TEE_Platform [TEE 管理平台 (Host)]
        B{Manager Service<br/>并发任务调度层 & 状态机}
        C[[Occlum Enclave Worker 1]]
        C2[[Occlum Enclave Worker N]]
    end

    subgraph Data_Owner [数据提供方]
        D[Data Connector<br/>加密通信终端]
    end

    %% 流程连接
    A -- "1.传入代码 (tar.gz)" --> B
    B -- "2.调度启动 (分配动态端口 & TaskID)" --> C
    B -.-> C2
    C -- "3.内部 Webhook 回调" --> B
    D -- "4.经由 RA-TLS 注入安全数据" --> C
    C -- "5.在主存中计算并返回结果" --> D

    %% 样式美化
    style A fill:#f96,stroke:#333,stroke-width:2px
    style B fill:#69f,stroke:#333,stroke-width:2px
    style C fill:#5fb,stroke:#333,stroke-width:4px
    style C2 fill:#5fb,stroke:#333,stroke-width:2px,stroke-dasharray: 5 5
    style D fill:#f96,stroke:#333,stroke-width:2px
```

## 平台部署手册

### 0. 准备 & 启动 TEE 管理端

首先需要在您的受信任云主机或物理机上（如 `192.168.0.248`）启动管理体系：
```bash
cd enclave-manager
go run main.go
# Server 会在 8081 端口启动，提供 Task 级调度
```

### 1. 编译并打包隔离区 App（代码提供方）

Occlum 的载荷必须为 `PIE` 类型的 Linux x64 二进制文件，我们使用原生 Go 构建安全内核并打包：
```bash
cd enclave-app

# 禁用 CGo 交叉编译
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -buildmode=pie -o enclave-app main.go

# 压缩为 Manager 支持的输入格式
tar -czvf enclave.tar.gz enclave-app
```

### 2. 发布任务至平台（代码提供方）

将最新的执行文件发往 TEE 平台启动隔离区：
```bash
cd code-connector
go build -o code-connector main.go
./code-connector ../enclave-app/enclave.tar.gz

# 控制台将输出分配到的 [Task ID] 以及 RA-TLS 连接的 [动态端口]
```

*(附：任意时刻想要监控任务进度时，可以访问或调用 `http://<MANAGER_IP>:8081/task-status?task_id=<TaskID>` 接口，以查看如 `DATA_RECEIVED` 或 `ENCLAVE_RUNNING` 的进度。)*

### 3. 数据运算与结算（数据提供方）

携带敏感数据向已就绪的隔离区进行直传解析（该过程基于底层验证机制，对宿主机平台本身免疫窃听）：
```bash
cd data-connector
go build -o data-connector main.go

# 发起运算请求： ./data-connector <敏感数据路径> <TEE平台IP>:<分配的动态端口>
./data-connector ../path/to/secret-data.csv 192.168.0.248:XXXXX
```
