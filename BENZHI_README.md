# BENZHI_README

这是一个基于 Go 实现的后端服务，用于承载 youth-rehab-ops 的业务处理、数据管理与稳定运行。

## 项目说明

- 项目：11DingKing/youth-rehab-ops
- 项目用途：Youth Rehab Operations is a backend service for handling youth summer-sport injuries from a venue report through professional return-to-training clearance. It keeps operational facts, clinical decisions, guardian communications, scheduling restrictions, overrides, and sensitive-record access separate and auditable.
- Go 工具链：`golang:1.25.0`
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
./build_benzhi_docker.sh benzhi-task-48-amd64 linux/amd64
./build_benzhi_docker.sh benzhi-task-48-arm64 linux/arm64
docker run -it benzhi-task-48-amd64:latest
docker run -it --platform linux/arm64 benzhi-task-48-arm64:latest
```

## 题目验证命令

1. 预期退出码 1：`go test ./internal/service -run '^TestCancelledTriageDoesNotAdvanceIncident$' -count=1`
