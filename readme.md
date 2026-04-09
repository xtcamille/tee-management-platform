# TEE 管理平台工作流程

![Gemini_Generated_Image_je7vc6je7vc6je7v](https://cdn.jsdelivr.net/gh/xtcamille/TyporaImages//img/20260407160226831.png)



## 流程说明

1. **代码上传**：代码连接器将代码传入 TEE 管理平台
2. **启动执行**：TEE 管理平台启动 Enclave 执行代码
3. **数据传输**：数据连接器将数据传输给 Enclave 内正在运行的代码
4. **结果返回**：将数据处理结果返回给数据连接器

## 流程图

```mermaid
graph TD
    %% 定义参与方
    subgraph Dev [代码提供方]
        A[代码连接器]
    end

    subgraph TEE_Platform [TEE 管理平台]
        B{平台管理服务}
        C[[Enclave 安全区域]]
    end

    subgraph Data_Owner [数据提供方]
        D[数据连接器]
    end

    %% 流程连接
    A -- "1.传入代码" --> B
    B -- "2.启动并加载代码" --> C
    D -- "3.传输原始数据" --> C
    C -- "4.返回计算结果" --> D

    %% 样式美化
    style A fill:#f96,stroke:#333,stroke-width:2px
    style B fill:#69f,stroke:#333,stroke-width:2px
    style C fill:#5fb,stroke:#333,stroke-width:4px
    style D fill:#f96,stroke:#333,stroke-width:2px
```

## 部署

```bash
# 1. 编译代码连接器
cd code-connector
go build -o code-connector main.go

# 2. 编译数据连接器
cd ../data-connector
go build -o data-connector main.go

# 3. 启动 TEE 管理平台
cd ../enclave-manager
go run main.go

# 4. 启动代码连接器（传入处理代码）
./code-connector /path/to/your/code

# 5. 启动数据连接器（传入数据文件）
./data-connector /path/to/your/data.txt
```

