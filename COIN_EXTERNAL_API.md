# 旧币第三方支付接口与网页调试页

## 访问入口

- 调试页：`GET /coin-tool`
- 静态资源路径：`/app-assets/*`
- API 基础路径：`/v1/external/coin/*`

说明：调试页默认使用同源 `/v1` 调用接口。

## 接口 1：发起付款

- 路径：`POST /v1/external/coin/pay`
- 认证：请求体携带付款方账号密码（支持 `account` / `uid` / `user_id` + `password`）

### 请求字段

- 付款方身份：`account` 或 `uid` 或 `user_id`
- `password`：付款方密码
- 收款方身份：`to_account` 或 `to_uid` 或 `to_user_id`
- `amount`：正整数，范围 `1~1000000`
- `client_order_no`：建议必填，最大 64 字符，用于幂等
- `remark`：可选，最多 120 字符

### 返回说明

- `201`：创建成功（首次付款）
- `200`：幂等命中（已付过），会返回 `already_paid=true`
- `409`：`order_conflict`（同订单号但金额/收款对象冲突）

### 示例

```bash
curl -X POST "/v1/external/coin/pay" \
  -H "Content-Type: application/json" \
  -d '{
    "account":"payer_account",
    "password":"payer_password",
    "to_uid":"ABC123",
    "amount":100,
    "client_order_no":"order_20260209_001",
    "remark":"网页支付测试"
  }'
```

## 接口 2：收款方验证到账

- 路径：`POST /v1/external/coin/verify`
- 认证：请求体携带收款方账号密码（支持 `account` / `uid` / `user_id` + `password`）

### 请求字段

- 收款方身份：`account` 或 `uid` 或 `user_id`
- `password`：收款方密码
- 查询键：`transfer_id` 或 `client_order_no`（至少一个）
- 付款方过滤（可选）：`from_account` / `from_uid` / `from_user_id`

### 返回说明

- `received=true`：已到账
- `received=false` 且 `status=not_found`：未匹配到账

### 示例

```bash
curl -X POST "http://127.0.0.1:8080/v1/external/coin/verify" \
  -H "Content-Type: application/json" \
  -d '{
    "account":"payee_account",
    "password":"payee_password",
    "client_order_no":"order_20260209_001",
    "from_uid":"PAYER01"
  }'
```

## 调试页说明（/coin-tool）

页面包含两个独立表单：

- 发起付款
- 到账验证

并支持：

- 一键填充示例参数
- 实时显示 HTTP 状态码/耗时
- 展示 JSON 响应
