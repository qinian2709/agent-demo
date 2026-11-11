# Agent 测试评测台

给智能 Agent（尤其是带 Tool / MCP 的 Agent）**自动生成测试用例**，并**真实执行、检测效果**。

这不是日志链路工具。评测看的是：有没有调对工具、回答有没有命中预期、该拒绝的有没有拒绝、是否超时。

在线地址：[https://agent-test-platform.coolify.eeo-inc.com](https://agent-test-platform.coolify.eeo-inc.com)

---

## 做什么

1. **生成用例**：读取当前 Agent 已注册工具（天气、计算、搜索、第三方 MCP、自研订单 MCP），按工具目录生成基线用例；可选再用 LLM 补模糊输入、误用陷阱、安全拒绝等边界用例。
2. **自动执行 + 打分**：逐条把用例丢给真实 Agent，断言：
   - 工具路由（该调 `weather` 却调了 `search` → Fail）
   - 关键词 / 计算结果
   - 行为标签（`should_refuse` / `should_ask_clarification` / `should_call_<tool>`）
   - 延迟上限

被测对象就是本仓库自带的 Tool Agent，页面里的「对话沙箱」只用来手工复现。

---

## 怎么用

打开 [评测台](https://agent-test-platform.coolify.eeo-inc.com/eval.html)：

1. 点 **生成测试用例**（可勾选「用 LLM 补边界用例」，也可在输入框写补充要求）
2. 点 **执行评测**
3. 看通过率，点开一行看实际调用了哪些工具、回复原文、失败原因

本地：

```bash
cd agent-demo
go test ./eval
go run .
# 打开 http://localhost:3000/eval.html
```

默认端口读环境变量 `PORT`，未设置时为 `3000`。

---

## API

| 方法 | 路径 | 作用 |
| :--- | :--- | :--- |
| GET | `/api/tools` | 当前 Agent 工具清单（生成器用） |
| POST | `/api/eval/generate` | 生成用例。body: `{ "use_llm": true, "focus": "多加订单异常" }` |
| POST | `/api/eval/run` | 执行评测。body: `{ "cases": [...] }`；`Accept: text/event-stream` 或 `?stream=1` 可边跑边推 |
| GET | `/api/eval/cases` | 最近一次生成的用例 |
| GET | `/api/eval/report` | 最近一次评测报告 |
| POST | `/api/chat` | 被测 Agent 对话接口（评测内部也走同一套执行逻辑） |

单条用例字段（与 `web-test-agent/templates/ai-agent-test-set.md` 对齐）：

```json
{
  "id": "T-weather-01",
  "category": "工具调用",
  "input": "今天北京天气怎么样？",
  "expected_keywords": ["温度", "气温", "℃"],
  "keyword_mode": "any",
  "expected_tools": ["weather"],
  "expected_behavior": "should_call_weather",
  "max_latency": 15,
  "tool_policy": "required",
  "risk": "high"
}
```

判定：`Pass` / `Partial`（例如超时） / `Fail`（工具调错、该拒绝没拒绝） / `Blocked`（执行失败）。

---

## 目录

| 路径 | 说明 |
| :--- | :--- |
| `eval/` | 用例生成、执行、打分 |
| `eval.html` | 评测台页面 |
| `agent/tool_agent.go` | 被测 Agent，`RunDetailed` 返回 tool_calls 供断言 |
| `tools/` | 本地工具 + MCP 工具 |
| `chat.html` | 手工对话沙箱 |

给其他 Agent 用时：实现同样的「工具清单 + 一次调用返回 tool_calls」即可接入这套生成器和跑手。不必接日志追踪。
