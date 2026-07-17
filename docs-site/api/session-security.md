# 会话与登录安全

v0.12 使用短期 Access Token 和服务端可撤销 Session。浏览器不会把长期 Refresh Token 放到
页面可读存储中。

## 登录后的内容

`POST /api/v1/auth/login` 成功后：

- 响应中的 `access_token` 只在当前页面内存中使用；
- `campusos_refresh` 是 `HttpOnly`、`SameSite=Lax` Cookie，JavaScript 不能读取；
- `campusos_csrf` 是页面可读取的随机 Cookie，用于证明写请求来自本站页面。

每个 Access Token 带有 Session ID 和账号认证版本。后端会检查签名、Session 是否未撤销、账号是否
可用以及认证版本是否一致。旧格式 JWT 和 Refresh 凭据不能访问受保护接口。

## 刷新流程

页面加载或 Access Token 失效时，客户端执行：

```http
POST /api/v1/auth/refresh
X-CSRF-Token: <campusos_csrf Cookie 的值>
```

浏览器自动附带 HttpOnly Refresh Cookie。服务端把旧 Token 立即作废、写入新的摘要，并返回新的
Access Token。一个已经轮换的 Refresh Token 再次出现时，系统会撤销该 Token 家族的所有 Session，
防止凭据重放。

## 设备管理

| 接口 | 作用 |
| --- | --- |
| `GET /api/v1/auth/sessions` | 查看当前账号的已登录设备，不返回任何 Token。 |
| `DELETE /api/v1/auth/sessions/:id` | 注销一个设备。 |
| `POST /api/v1/auth/logout` | 退出当前设备。 |
| `POST /api/v1/auth/logout-all` | 退出全部设备，旧 Access Token 立即失效。 |

注销、注销全部和删除设备是写操作，必须携带 `X-CSRF-Token` 和 Access Token。

## 部署注意事项

生产环境设置 `AUTH_SESSION_IP_HASH_SECRET`，并保持 `AUTH_REFRESH_BODY_COMPAT=false`。前端、
脚本和插件都不得把 Access 或 Refresh Token 放进 localStorage、URL、日志或导出文件。

详细接口、兼容窗口和迁移演练见仓库的
[v12 会话与 Token 安全流程](https://github.com/javencpdd/CampusOS/blob/main/docs/api/v12%E4%BC%9A%E8%AF%9D%E4%B8%8EToken%E5%AE%89%E5%85%A8%E6%B5%81%E7%A8%8B.md)。

