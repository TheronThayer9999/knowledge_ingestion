# Đợt 26 — Chart dựng từ chunk đã dẫn, LLM chỉ chọn loại biểu đồ

Ngày: 2026-10-27 · Trạng thái: kế hoạch (chưa code) · Nối: Đợt 25 (`archive/2026-10/` — trả lời như chuyên gia + eval đa provider), thiết kế lấy từ `RAG_RESEARCH.md` §6.9

## 1. Yêu cầu đã nhận

Người dùng: "test ok các tính năng, nghiên cứu các tính năng khác nhé, việc test logic API rất quan trọng, các case, các loại data, output các thứ trả lời mark, **vẽ chart và cho llm quyết định vẽ chart nào**".

Chốt phạm vi thành một câu: **câu trả lời chat có thể kèm biểu đồ; model được quyền chọn "dựa trên nguồn [n], vẽ loại gì, tiêu đề gì"; nhưng mọi con số trong biểu đồ do backend lấy từ chunk đã dẫn, model không được gõ số vào spec.**

Ràng buộc: commit message tiếng Anh; kế hoạch commit trước khi code; verify trước khi commit; không tự ý mở browser test UI (người dùng tự test, chỉ để checklist).

## 2. Vì sao phải "server dựng spec" — và vì sao bản §6.9 được đơn giản hoá

§6.9 đã chốt lý do: *chart sai số trông rất thuyết phục*, nguy hiểm hơn đoạn văn sai. Nên spec là đầu ra của code đã kiểm chứng, không phải đầu ra của model.

Một điểm của §6.9 viết lại cho gọn: mục đó ghi "nạp lại chunk theo `(article_id, chunk_index)` từ Qdrant, Collector chỉ là fallback khi cùng lượt". Thực tế khi tool chạy **trong cùng lượt**, `HitCollector.Hits` đã chứa nguyên văn `Text` của chunk (Qdrant payload — `ScoredChunk.Text`), nên nạp lại là thừa một round-trip gRPC. Ta giữ nguyên tinh thần "số phải từ chunk", chỉ đổi chỗ lấy chunk:

- **v1 (đợt này): nguồn duy nhất là collector của lượt đang chạy.** `n` không có trong collector ⇒ tool từ chối, trả note bảo model đi tìm nguồn trước (`knowledge_search`/`read_sheet`) rồi mới xin chart.
- Đường "vẽ lại chart từ cite của message đã lưu" (khác lượt) **không làm đợt này**: khi nào cần thì thêm một lần `ScrollRange(article_id, from=chunk_index, limit=1)` — đã ghi ở §8 để đừng quên.

## 3. Thiết kế backend

### 3.1 Tool mới `chart_from_cite` (`src/agent/eino/chart_from_cite.go`)

Khuôn theo đúng `list_sheets.go` (đã có test khuôn mẫu ở `sheet_tools_test.go`): `InferTool` + args struct có tag `jsonschema` + desc tiếng Việt có mục KHI NÀO GỌI / KHÔNG GỌI / CÁCH DÙNG / VÍ DỤ + `ScopeFrom(ctx)` bắt buộc + `Collector.MarkSearched()`.

```go
type ChartFromCiteArgs struct {
    N     int    `json:"n"    jsonschema:"..."` // số nguồn [^n] đang có trong lượt
    Kind  string `json:"kind" jsonschema:"..."` // bar | line | pie
    Title string `json:"title" jsonschema:"..."`
}
```

- `n` là **số trích dẫn model nhìn thấy**, không phải `article_id`/`chunk_index` — model không được quyền đụng ID kỹ thuật (mục 10 prompt, Đợt 22).
- Trả về cho model JSON **ngắn**: `{ok, n, kind, title, points: [{label, value}], caption, note}`. Model chỉ cần biết có bao nhiêu điểm và nhãn là gì để viết câu chữ; **spec đầy đủ không quay lại model** (tránh model copy số sang chỗ khác rồi sửa sai).
- Tool ghi chart vào collector (`AddChart`) — `ChatResponse.charts` dựng từ đó, không dựng từ lời model.

### 3.2 `HitCollector` (`src/agent/eino/context.go`) thêm hai thứ

- `Cite(n int) (*domain.ScoredChunk, bool)` — tra theo đúng cách `buildCites` đánh số (n = index+1 trong `Hits`), đọc dưới lock.
- `AddChart(*CollectedChart)` + slice `Charts []*CollectedChart`, có mutex như `Add`; `Snapshot()` đổi chữ ký thành trả thêm charts (hoặc thêm `SnapshotCharts()` để không đụng 3 chỗ đang gọi `Snapshot` — **chọn `SnapshotCharts()`**, ít gãy).

`CollectedChart` = `{N int, Kind string, Spec map[string]any, Caption string}` — Spec là ECharts `option`, kiểu map để không phải kéo lib Go về.

### 3.3 Trích số từ text chunk (`src/agent/eino/chart_numbers.go`)

Hai dạng text thật đã thấy trong cites của batch Đợt 25 (bằng chứng trong `loadtest/expert_eval_complex_results.json`):

1. **Bảng Excel** — `## Sheet: Luong` + các dòng `| Mã NV | Họ và tên | ... | 800000 | ... |`. Nhãn = ô đầu tiên của dòng (`NV01`), giá trị = ô số. Excel trích ra **số trần** (`800000`, `21.5`) nên không lo định dạng.
2. **Văn bản/PDF** — `Phí 150.000 đồng`, `tỷ lệ 0,05%/ngày`, `22,5 ngày`. Đây là bẫy locale §6.9(c) nêu: **dấu `.` là phân cách nghìn, dấu `,` là thập phân**.

Hàm pure, dễ test:

```go
func ParseVNNumber(s string) (float64, bool)   // "1.500.000" -> 1500000; "0,05" -> 0.05; "800000" -> 800000; "abc" -> false
func ExtractSeries(text, kind string) ([]DataPoint, string) // points + note (lý do từ chối)
```

Luật cắt series (quyết định ở đây để code không phải đoán):

- Tách dòng theo `\n`; chỉ nhận dòng là hàng bảng thật (`|` ≥ 3 ô) — hàng `## Sheet:` và hàng phân cách `|---|` bị loại.
- Với mỗi hàng: nhãn = ô không phải số đầu tiên; giá trị = **cột số có nhiều hàng lấy được số nhất** (chọn 1 cột duy nhất cho cả series — không trộn cột, vì trộn là vẽ láo). Ô chứa `Tổng cộng`/`Cộng` bị loại khỏi series để không vẽ hai lần một tổng.
- `pie`: cần nhãn + giá trị, không cần thứ tự. `bar`: như pie nhưng giữ thứ tự hàng. `line`: chỉ nhận khi các nhãn **tăng dần được thành dãy số hoặc ngày/tháng** (`01/07/2025`, `Q3`, `2024`, `Tháng 10`) — không đủ điều kiện thì rơi về `bar` kèm note "kind line không hợp với dữ liệu phân loại, đã vẽ bar".
- Trần 12 điểm, sàn 2 điểm. Ngoài khoảng ⇒ từ chối, note cho model.

Từ chối là **kết quả bình thường**, không phải lỗi: tool trả `{ok: false, note: "..."}` để model chuyển sang trả lời bằng văn + bảng, đúng bài học Đợt 25 (X-01 model lộ `sheet_count` vì tool không nói rõ giới hạn).

### 3.4 Dựng spec và cổng kiểm tra

- `buildEChartOption(kind, title, points)` trả `map[string]any` với đúng hai shape: `xAxis.category` + `series[0].{type, data}` cho bar/line; `series[0].{type: "pie", data: [{name, value}]}` cho pie.
- **Cổng validate trước khi ghi collector**: duyệt `spec` bằng đệ quy, mọi `float64` phải khớp một giá trị trong `points` (so sánh `==` trên float đã round 6 chữ số). Đây là nơi chống *chính code của mình* thêm số về sau — không phải chống model, vì model không touch spec. fail ⇒ bỏ chart, log `logs.Error`, trả note.
- `caption` tự sinh: `"Nguồn: [^n] · <title>"` (khuôn `[^n]` để FE bấm được sang source panel như chip hiện tại; `normalizeCitations` không cắt vì n nằm trong trần cites).

### 3.5 DTO + lưu DB + wiring

- `src/controller/dtos/chat_dto.go`: thêm `ChatChart{N, Kind, Spec map[string]any, Caption}` và field `Charts []*ChatChart \`json:"charts"\`` vào `ChatResponse` — **mảng rỗng chứ không nil**, FE không phải xử lý hai hình dạng.
- `domain`: message có thêm cột `charts jsonb` (gorm `type:jsonb;serializer:json` như `cites` đang làm — đọc lại `domain/message.go` để bắt kịp cách `cites` map, **không** tự chế kiểu mới). AutoMigrate tự thêm cột; `schema/jobs.sql` không liên quan, không sửa.
- `port.ChatTurn` thêm `Charts`; `chat_session_service.go` + `chat_message_repository.go` mang charts theo khi ghi và khi đọc (`ListRecent` trả đủ để mở lại phiên vẫn thấy biểu đồ).
- `src/agent/usecase/chat_service.go`: `buildCites` đã có chỗ đối xứng — thêm `buildCharts(charts []*eino.CollectedChart) []*dtos.ChatChart`, gọi cạnh `buildCites`, và `Chat()` gắn `Charts:` vào response. Chú ý `function_budget_test.go` (`src/quality`): `Chat()` đang sát trần, vì thế dựng charts **trong hàm riêng**, không viết dài ra tại chỗ.
- `src/loader/agent.go`: tool mới dựng bằng `NewChartFromCiteTool()` rồi đăng ký qua `ToolDeps`/`NewDefaultTools` (`tools.go`) — loader không phình.
- Swagger đổi hình dạng `ChatResponse` ⇒ chạy `swag init -g cmd/apis/main.go` (không sửa tay `docs/`).

### 3.6 Công khai tắt được

`configs` thêm `agent.chart_enabled` (bool, default true). `false` ⇒ không đăng ký tool, `ChatResponse.charts` luôn rỗng. Lý do: tính năng mới đụng cả 4 tầng, cần một nút dừng không phải revert.

## 4. Thiết kế frontend

- Phụ thuộc mới: `echarts` + `ngx-echarts@21` (hỗ trợ Angular 21 + signals). **Cấm import echarts ở top-level** — `angular.json` chốt `initial` 600kB warn / 1MB error mà echarts core đã ~900kB min. Component chart dùng `await import('echarts/core')` + `echarts/chart/BarChart|PieChart|LineChart` + `component/Grid,Tooltip,Legend` trong `ngOnInit` bất đồng bộ, render placeholder khi chưa xong. Verify bằng `npm run build`: chunk chat phải tách riêng, `initial` không đổi.
- `shared/chart/echarts-chart.ts` (+ template/scss inline): input `options`, `caption`, `n`; output `citeClick`. Cao 260px, `aria-label` mô tả biểu đồ cho screen reader (ESLint a11y đã bật).
- `core/chat/chat.models.ts`: thêm `ChatChart`; `chat.ts` map `response.charts` vào turn; `chat.html`: `@for (chart of turn.charts)` **dưới** phần answer, mỗi chart có chip "Nguồn: [n]" reuse handler nhảy source hiện có (`onCiteRefClick` cùng logic).
- Message cũ (load lại phiên) có `charts` thì render y như vừa hỏi — charts nằm trong `messages.charts` nên không cần thêm request.
- i18n: thêm key vào `vi.ts` rồi `en.ts` (kiểu `Record<TranslationKey, string>` bắt buộc đủ, thiếu là build đỏ).
- Budget style: thêm style chart vào `chat.scss` có thể vượt 14kB — **được phép nâng lên mức hợp lý tiếp theo** theo chủ project đã cho (ghi rõ con số mới vào `angular.json`).

## 5. Test — mũi chính của đợt này (người dùng: "test logic API rất quan trọng")

### 5.1 Go (unit, không cần server)

- `chart_numbers_test.go`: bảng định dạng tiếng Việt (`1.500.000`, `21,5`, `0,05%`, `800000`, `1,200,000` kiểu quốc tế, `abc`, `1.2.3` hỏng); chọn cột; loại `Tổng cộng`; `pie`/`bar`/`line` rơi kind; sàn 2 / trần 12; text PDF không bảng → từ chối kèm note.
- `chart_from_cite_test.go` (khuôn `sheet_tools_test.go`): `n` không tồn tại → `ok:false`; `n` vượt trần collector; scope thiếu → error; chart vào collector đúng shape; caption.
- Test cổng validate: cố tình thêm `12345` vào spec trước khi ghi → bị từ chối.
- `chat_service`: response có `charts` rỗng khi không có chart (kiểu mảng, không null); persist + đọc lại qua `ChatTurn.Charts`.
- `src/quality/function_budget_test.go` phải vẫn xanh (hàm mới đều < 60 dòng).

### 5.2 Harness (đo thật, không chỉ đo "có chart")

`loadtest/expert_eval.py`:
- Lớp 0 thêm lỗi `CHART_DATA_UNSOURCED`: mỗi `value` trong `spec.series[].data` phải truy được từ **snippet của cite `n`** (dùng lại `num_variants` đã sửa ở Đợt 25 — số `21.5` và `21,5` phải khớp nhau).
- `check_layer0` đọc `data["charts"]`; `post_chat` đã trả nguyên payload nên chỉ cần thêm field.
- `--selftest` thêm 3 mục: chart khớp nguồn → pass; chart lệch một số → `CHART_DATA_UNSOURCED`; `charts: []` → không lỗi.
- Case chạy thật: thêm `X-30..X-35` vào `docs_test_case_and_data/testcase/test_case_complex_data.md` (đã có khung trong `chat_session_uiux_va_demo.md` §9), trong đó **hai case phải từ chối vẽ** (yêu cầu vẽ đường thẳng cho dữ liệu phân loại, yêu cầu vẽ từ nguồn không có số) — từ chối đúng mới là tính năng đạt.

### 5.3 FE

`npm run lint` + `npm test` + `npm run build`; spec cho component chart (mapping input, emit citeClick, placeholder khi lazy load chưa xong). Checklist UI tự test viết thêm vào `chat_session_uiux_va_demo.md` §7.

## 6. Thứ tự làm (mỗi bước verify rồi mới commit)

1. `chart_numbers.go` + unit test (chưa đụng ai, an toàn nhất).
2. `chart_from_cite.go` + collector (`Cite`, `AddChart`, `SnapshotCharts`) + test tool.
3. DTO `ChatChart`/`ChatResponse.Charts` + `domain` cột `charts jsonb` + repository/service + `swag init`.
4. Wiring `tools.go`/`loader/agent.go` + `agent.chart_enabled` + config.
5. FE: dep + lazy component + models + template + i18n.
6. Harness: lớp 0 chart + selftest + case X-30..X-35; chạy batch thật xoay 3 provider trên đúng bộ fixture Đợt 25 (`--skip-upload --category-id 130`).
7. Docs: cập nhật `chat_session_uiux_va_demo.md` (§9 từ "chưa làm" thành hướng dẫn demo), `RAG_RESEARCH.md` §6.9 ghi chú quyết định bỏ round-trip Qdrant.

## 7. Tiêu chí hoàn thành

- [ ] Không có đường nào để số từ model vào spec (chỉ `n`/`kind`/`title` là đầu vào model).
- [ ] `charts` có mặt trong `ChatResponse`, trong DB (`messages.charts`), và khi mở lại phiên.
- [ ] Unit test phủ định dạng số tiếng Việt + các nhánh từ chối; `go build`/`vet`/`lint`/`test` xanh.
- [ ] `expert_eval --selftest` đạt, `CHART_DATA_UNSOURCED` khoá được bằng chứng lệch số.
- [ ] Batch thật: ≥ 1 chart thật từ Excel được chứng minh + ≥ 1 lần từ chối đúng; bảng verdict per provider.
- [ ] `npm run build`: `initial` không đổi (echarts nằm ở chunk riêng), lint + vitest xanh.
- [ ] Không sửa `docs/` tay; `swag init` sau khi đổi annotation; `codegraph sync` sau khi sửa Go.
- [ ] `## Kết quả` điền + archive + dòng `plans/README.md`, commit tiếng Anh.

## 8. Cắt ra khỏi đợt này (ghi để không thất hứa)

- **Mermaid** (``` ```mermaid ``` trong answer + `securityLevel: 'strict'`) — chưa làm; model muốn vẽ sơ đồ thì nói thẳng là không vẽ được.
- **`stats_summary`** (gonum: count/min/max/mean/median/p95 + cảnh báo n < 5) — để đợt sau, vì nó là "output dạng số" chứ không phải chart, và Đợt 25 mới chứng minh phần trích số đáng tin hơn phần suy luận.
- **Vẽ chart từ cite của message đã lưu (khác lượt)** — cần một lần `ScrollRange` theo `(article_id, chunk_index)`; chỉ làm khi có người dùng thật bấm "vẽ lại".
- **Style linter + `prompts/style_vi.md`** (§6.9 d) — chưa.
- **`usage` per message** — vẫn defer (S3).

## 9. Rủi ro

| Rủi ro | Xử lý |
|---|---|
| Model gọi `chart_from_cite` trước khi có nguồn, rồi nài `n=1` bừa | Tool trả `ok:false` + note "chưa có nguồn [n], hãy search trước"; cổng 0 của harness bắt `NO_CITES` sẵn |
| Cột số chọn nhầm (bảng có 2 cột số: `Lương cơ bản` và `Thuế`) | Luật "chọn cột có nhiều hàng lấy được số nhất" + trả `note` nói đã chọn cột nào để model tự sửa nếu sai ý người dùng |
| Bundle FE nổ trần `initial` 1MB | Lazy `import()` echarts, kiểm bằng `npm run build` ngay bước 5 |
| Cột `charts jsonb` mới làm fail app trên DB cũ | gorm AutoMigrate thêm cột NULL; đọc `NULL` → mảng rỗng; không backfill |
| Vẽ từ OCR sai (RAG_RESEARCH #1: bảng OCR sai mà chart đẹp) | Điều kiện vẽ là chunk có bảng extract đúng — Excel pipe-line an toàn; PDF scan thì số nằm trong văn, dễ chọn sai ⇒ test case X-34 kỳ vọng **từ chối** |
| Feature đụng 4 tầng, hỏng giữa đợt | Cờ `agent.chart_enabled = false` là nút dừng; thứ tự §6 để bước 1–2 không ảnh hưởng hành vi cũ |
