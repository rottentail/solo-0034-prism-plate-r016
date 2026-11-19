# HTTP契约

所有写入场景的JSON入口拒绝未知字段和尾随内容，最大8MiB。服务返回JSON错误；404为对象不存在，409为版本或生命周期冲突，429为成像容量不足，400为不符合输入契约，504为超时。未知路由由HTTP框架返回404。

| 方法与路径 | 输入及结果 |
|---|---|
| GET /healthz | ready |
| POST /v1/plates | id,title,document →201版面 |
| GET /v1/plates/{id} | 完整稿本历史 |
| PUT /v1/plates/{id} | expected_revision,document →追加稿本 |
| POST /v1/plates/{id}/branch | revision,id,title →201独立版面 |
| POST /v1/plates/{id}/archive | 禁止修订；历史仍可读取 |
| POST /v1/plates/{id}/render?revision=1&format=png | 201成品元数据；format也可为svg |
| POST /v1/plates/{id}/exposures?revision=1&format=png | 202瞬态任务 |
| GET /v1/exposures/{id} | queued/running/succeeded/failed/cancelled |
| DELETE /v1/exposures/{id} | 202取消请求 |
| GET /v1/artifacts/{id} | 图片；ETag为成品字节摘要；X-Scene-Digest为稿本摘要 |
| DELETE /v1/artifacts/{id} | 撤销成品 |
| POST /v1/proofs | id,artifact →201 open校样 |
| PUT /v1/proofs/{id} | state为accepted或rejected，note为说明 |
| GET /v1/plates/{id}/bundle | 含完整稿本历史的封包JSON |
| POST /v1/bundles | 原样封包；201导入，重名409 |

成品响应中的data为null，避免重复传输二进制；GET成品获得实际内容。If-None-Match精确匹配强ETag时返回304。成功校样可从持久化快照确认，当前版本没有校样列表/详情接口。GET版面返回完整历史，服务没有全局列表接口。

## 图层效果契约

图层可选 `effects` 对象，缺省或省略表示无效果；空对象 `{}` 与未知字段一律 400。效果作用于整层合成结果，准入范围如下，越界值在编译阶段拒绝而不是静默夹取：

| 字段 | 含义 | 范围 |
|---|---|---|
| `blur.radius` | 高斯模糊 σ，画布像素 | `(0,32]`，有限数 |
| `shadow.color` | 投影 sRGB 直通色 | 各通道 `[0,1]`，alpha 必须 `>0` |
| `shadow.offset.x/y` | 投影位移，画布像素 | 每轴 `[-32,32]`，有限数 |
| `shadow.radius` | 投影高斯 σ，画布像素 | `[0,32]`（0 为硬投影） |

顺序固定为层体模糊 → alpha 投影（置于层体之下）→ 图层 opacity/blend。滤镜卷积预算独立于边遍历预算：`effect_taps × 宽 × 高 ≤ 200,000,000`，超限返回 400（错误文本 `layer effects exceed bounded filter work`）。SVG 成品必须包含与 PNG 同源参数的 `<filter>`，缺失滤镜即视为移交失真。
