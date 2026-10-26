# Đợt 8 — Chọn provider/model từ CLI + prompt bảng tính nạp theo điều kiện

## Bối cảnh

Hiện muốn đổi LLM phải sửa `configs/config.local.json` rồi restart: cứng, dễ sửa nhầm file
không được commit, và không có cách nào xem nhanh "đang cấu hình provider nào, provider nào
thiếu key". Prompt chat cũng chỉ có một file `prompts/chat_system.md`: quy tắc bảng tính
(Excel) nằm lẫn trong prompt chung nên câu hỏi về hợp đồng cũng phải "đọc" 6 khối luật sheet
vô ích, còn câu hỏi về Excel thì luật lại bị loãng giữa 250 dòng.

Phát hiện phụ khi đọc code: `prompts/chat_system.md` có 2 placeholder `{ngày_hiện_tại}` và
`{label}` nhưng **không chỗ nào render** — model đang nhận nguyên văn chuỗi `{ngày_hiện_tại}`
trong system prompt (quy tắc "đối chiếu ngày hôm nay" ở mục 11 mất căn cứ). Đợt này sửa luôn.

## Nghiên cứu: các repo nổi tiếng chọn provider/model thế nào

| Repo / CLI | Cách chọn | Điểm đáng học |
|---|---|---|
| aider (`aider --model`, `--list-models`) | 1 chuỗi `provider/model` kiểu litellm + lệnh liệt kê, gõ sai thì gợi ý model gần đúng | Discovery mode (`--list-models`) + fail-fast có gợi ý |
| `llm` (Simon Willison) | `-m/--model`, subcommand `llm models`, env `LLM_MODEL` | env làm lớp giữa: flag > env > config |
| LiteLLM / OpenRouter | model id dạng `provider/model`, `--model` thắng config yaml | Một nguồn chân lý cho id model, CLI đè file |
| Ollama | `ollama run <model>` + `ollama list` | Liệt kê trước, chọn sau |

Hai kết luận:

1. **Ưu tiên flag > env > file config > default** là quy ước chung (12-factor) — áp đúng thứ tự này.
2. Chuỗi gộp `provider/model` **không dùng được ở repo này**: model của 9router là
   `cl/nvidia/nemotron-3.5-super-vl:free`, OpenRouter là `nvidia/nemotron-3.5-super-vl:free`
   — tách theo dấu `/` đầu tiên sẽ cắt sai. Vì vậy dùng **hai flag riêng** `-provider` và
   `-model`, không đẽo theo litellm.

## Phần A — CLI/env/config cho provider + model

### A1. `src/config/config.go`

- `Load(path string, opts ...LoadOption) (IConfig, error)` (variadic, caller cũ không phải sửa).
- `type LoadOption func(*appConfig) error` + `WithLLMSelection(provider, model string) LoadOption`.
- Thứ tự áp dụng sau khi đã validate map `llms.providers`:
  1. file config (`llms.default_provider`, `providers[slug].model`)
  2. env `LLM_PROVIDER` / `LLM_MODEL`
  3. options (flag) — đè env
- Sau khi áp: provider phải tồn tại trong `llms.providers` → sai thì lỗi kèm danh sách slug
  (aider-style). Model rỗng thì **không** ghi đè model của provider.
- Fail-fast thêm: provider được chọn mà `model` vẫn rỗng → lỗi bảo thêm `-model` hoặc khai
  `model` trong config (thay vì để request đầu tiên mới 400).
- `FormatLLMs(cfg IConfig) string`: bảng provider (slug, type, model, base_url, `key=set|missing`),
  đánh dấu provider mặc định. **Không in api_key.**
- Env đọc trong `Load` (giống `GROQ_API_KEY`) nên worker cũng ăn mà không phải sửa main.

### A2. `cmd/apis/main.go` và `cmd/worker/main.go`

Thêm 3 flag, khai báo cạnh `-configs` trong `init()`:

```
-provider   slug provider muốn dùng (đè llms.default_provider; env LLM_PROVIDER)
-model      model muốn dùng (đè model của provider đó; env LLM_MODEL)
-list-llms  in danh sách provider/model đã cấu hình rồi thoát (không khởi động server)
```

`main()`: load config (kèm `WithLLMSelection`) một lần → nếu `-list-llms` thì in rồi return →
ngược lại `fx.Provide(func() config.IConfig { return cfg })`, log `llm selected
provider/model` lúc start. Không dựng 2 nguồn config (trước đây Load nằm trong closure fx).

## Phần B — Prompt bảng tính nạp có điều kiện

### B1. Tách file

- `prompts/chat_system_sheets.md` (mới): toàn bộ luật Excel đang nằm rải trong
  `chat_system.md` (Bước 1d, "Thông tin tối thiểu" dòng bảng dữ liệu, ngoại lệ trần 8 lượt
  ở Bước 2, mục 7 dòng Excel, 3 bullet + bullet đếm hàng của mục 11) **+ luật mới** (B3).
- `prompts/chat_system.md`: bỏ các khối trên, giữ câu chung ("đọc theo trang, liệt kê mục …").
  Bỏ luôn dòng `{label}` (placeholder chết, classifier chạy sau khi agent xong nên không có
  nhãn tại thời điểm chạy — thay bằng câu không cần placeholder).

### B2. Cơ chế nạp theo điều kiện (đúng API Eino, không hack)

`adk.ChatModelAgentConfig.Instruction` cố định lúc dựng agent, nhưng middleware
`BeforeAgent(ctx, runCtx)` được gọi mỗi lượt và **sửa được `runCtx.Instruction`**
(`eino@v0.9.21/adk/handler.go:87-142`, `chatmodel.go:812`). Dùng đúng hook đó:

- `src/agent/eino/instruction.go` (mới):
  - `type InstructionSet struct { Base, Sheets string }`
  - `type PromptVars struct { Today string; Sheets bool }`
  - `(InstructionSet).Render(v PromptVars) string` — thay `{ngày_hiện_tại}`, nối `Sheets`
    khi `v.Sheets` (kèm tiêu đề `# Quy tắc riêng: bảng tính`).
  - `WithPromptVars(ctx, v)` / `promptVarsFrom(ctx)` (fallback: hôm nay, không sheet).
  - `IsSpreadsheetQuestion(message string, history []dtos.ChatHistoryItem) bool` — keyword
    không phân biệt hoa thường trên câu hỏi hiện tại + **4 message gần nhất của mọi role** (câu
    trả lời “Theo sheet Bảng lương …” của assistant là tín hiệu đủ mạnh cho câu nối tiếp):
    `excel, xlsx, xls, spreadsheet, sheet, trang tính, bảng tính, bảng dữ liệu, bảng `.
    Ghi rõ trong comment: **thừa thì vô hại (thêm luật), thiếu mới sai** → chọn thiên về bật.
    (`bảng ` có dấu cách để “Bảng lương tháng 9…” — không nói Excel — vẫn bật; đây là lý do
    bản nháp chỉ quét user turn bị thay.)
- `src/agent/eino/agent.go`:
  - `NewChatAgent(model, instr InstructionSet, tls ...einotool.BaseTool)`.
  - `promptInjector` implement `adk.ChatModelAgentMiddleware` (embed
    `*adk.BaseChatModelAgentMiddleware`), `BeforeAgent` render lại instruction theo ctx.
  - `Handlers: []adk.ChatModelAgentMiddleware{injector, summ}`.
  - `Run(...)` set `ctx = WithPromptVars(ctx, PromptVars{Today: time.Now().Format("02/01/2006"),
    Sheets: IsSpreadsheetQuestion(query, history)})`.
- `provider.go` / `loader/agent.go`: đổi tham số `system string` → `system InstructionSet`,
  loader truyền `InstructionSet{Base: prompts.System, Sheets: prompts.Sheets}`.
- `src/agent/synthesis/chat_synthesizer.go`: `Prompts{System, Sheets}` + load
  `chat_system_sheets.md` fail-fast (cùng quy ước, không có bản mặc định trong code).

### B3. Luật mới cho LLM

`chat_system_sheets.md` (15 luật, gồm cả 6 luật cũ chuyển sang, theo đúng thứ tự trong file):

1. Liệt kê sheet trước khi đọc dữ liệu (bỏ qua chỉ khi đã liệt kê trong cùng lượt).
2. Chốt tên cột theo đúng hàng header tool trả; file không có header thì hàng đầu vẫn là dữ liệu.
3. Đọc theo hàng, còn mốc đọc tiếp thì đọc tiếp; cần cả bảng thì đọc hết rồi mới kết luận + nêu phạm vi hàng.
4. Đọc sheet **không** tính vào trần 8 lượt gọi tool ở bước 2.
5. Ô trống/công thức: nói rõ ô không có giá trị đọc được, không suy từ ô khác, không coi là số 0.
6. Không cộng/trừ/trung bình gộp hai sheet mà chưa nêu rõ từng phần lấy từ sheet nào.
7. Số sheet/số hàng lấy từ trường tổng của tool, không tự đếm dòng nhìn thấy.
8. Giữ nguyên đơn vị/định dạng của ô; phải làm tròn thì nói rõ đã làm tròn.
9. Trích dẫn "(Theo kết quả đọc sheet: <file>, sheet <tên>, hàng a–b)"; chỉ dùng `[^n]` khi tool trả số nguồn n.
10. Không dựng bảng/danh sách mới thay dữ liệu gốc; rút gọn phải nói rõ và giữ đủ cột cần.
11. So sánh hai file: liệt kê sheet cả hai trước, đối chiếu cùng bộ tiêu chí, nêu sheet chỉ có một bên.
12. Câu nối tiếp giữ đúng file/sheet lượt trước; chưa chốt được sheet thì nói rõ thay vì đoán.
13. Tool báo không phải Excel/chưa trích/không có sheet: nêu trạng thái rồi chuyển sang tìm kiếm/đọc trang, không gọi lại y nguyên.
14. Không kết luận "không có số liệu" chỉ vì search trả mảnh bảng vỡ cột khi chưa thử `read_sheet`.
15. Câu hỏi gọi tên file: tra ID của đúng file đó, không lấy sheet của file khác cùng chủ đề.

`chat_system.md` thêm 3 luật chung (không dính Excel):

- Số `[^n]` giữ nguyên theo từng lượt gọi tool trong cả câu trả lời, không đánh lại số.
- Người dùng yêu cầu định dạng đầu ra (bảng/danh sách/số dòng) thì theo đúng định dạng,
  nhưng không bịa thêm dữ liệu để lấp cho đủ.
- Câu đã trả lời rồi mà được hỏi lại: nhắc lại ngắn rồi chỉ bổ sung phần mới.

## Test

- `src/config/llm_selection_test.go`: env đè file; flag đè env; provider lạ → lỗi có danh sách
  slug; `-model` một mình áp lên default provider; `FormatLLMs` không lộ api_key.
  Fixture JSON tạm (đủ field bắt buộc: app.port, database.host, embedding.*, qdrant.host,
  redis.host, rabbitmq.host, jwt.secret, llms.*).
- `src/agent/eino/instruction_test.go`:
  - `TestIsSpreadsheetQuestion` (VI + EN + nối tiếp qua history + câu phủ định).
  - `TestRenderInstructionFillsToday` — output không còn `{`, có ngày.
  - `TestRenderInstructionAppendsSheetsOnlyWhenRelevant`.
  - `TestSheetRulesLiveOnlyInFragment` — đọc `../../../prompts/chat_system.md`, cấm chứa
    `sheet/Excel/bảng tính`; fragment phải chứa (khoá đúng yêu cầu "chỉ push khi liên quan").
- `src/agent/synthesis/prompts_test.go`: assert `Sheets` load được và có "sheet".
- `src/agent/eino/provider_test.go`: **9router vẫn chạy** — dựng provider
  `type=openai, base_url=http://localhost:20128/v1, model=cl/nvidia/nemotron-3.5-super-vl:free`
  (không gọi mạng), `BuildProviderAgents("9router", ...)`, `Resolve("", "")` trả đúng model
  còn nguyên dấu `/`; `Resolve("9RouTer ", "")` vẫn khớp (chuẩn hoá slug).

## Verify

`go build ./...`, `go vet ./...`, `golangci-lint run ./...`, `go test ./...`,
`go run ./cmd/apis -list-llms` (kiểm tra bằng mắt với `configs/config.local.json`, 9router
phải hiện `key=set`), `codegraph sync`.

## Commit dự kiến

1. `docs(plans): add batch 8 plan for llm cli selection and dynamic sheet prompt`
2. `feat(config): pick llm provider and model from flag, env or config`
3. `feat(agent): inject spreadsheet rules only for spreadsheet questions`
4. `docs(prompt): move sheet rules to a fragment and add output rules`
5. `docs(rag): record llm selection design and per-run prompt injection`

## Nghiệm thu

- [x] `-provider`/`-model`/`-list-llms` hoạt động trên api **và** worker; env cũng ăn.
- [x] Chọn provider sai → lỗi nêu danh sách slug, không boot nửa vời.
- [x] `chat_system.md` không còn luật Excel; `chat_system_sheets.md` chỉ được nối vào
      instruction khi câu hỏi liên quan bảng tính (có test khoá).
- [x] `{ngày_hiện_tại}` được render mỗi lượt; `{label}` chết đã bỏ.
- [x] 9router: `-list-llms` hiện đúng model `cl/...:free`, test Resolve xanh.
- [x] build/vet/lint/test sạch; `codegraph sync` chạy sau khi sửa code.

## Kết quả

Đã làm đúng phạm vi, verify sạch: `go build ./...`, `go vet ./...`, `golangci-lint run ./...`
(0 issues), `go test ./...` (toàn bộ package xanh, gồm 4 file test mới).

| Việc | File | Bằng chứng |
|---|---|---|
| Chọn LLM từ CLI/env | `src/config/llm_selection.go`, `config.go`, `cmd/apis/main.go`, `cmd/worker/main.go` | `go run ./cmd/apis -configs ./configs/config.local.json -list-llms` in `* 9router … key=set model=cl/nvidia/nemotron-3.5-super-vl:free`; `-provider GROQ -model llama-3.1-8b-instant` đổi đúng dòng có `*`; `-provider nope` → `llms: provider "nope" chưa cấu hình (đang có: 9router, groq, ollama, openrouter)` |
| Prompt tách + nạp có điều kiện | `prompts/chat_system.md`, `prompts/chat_system_sheets.md`, `src/agent/eino/instruction.go`, `agent.go`, `provider.go`, `src/loader/agent.go`, `src/agent/synthesis/chat_synthesizer.go` | `TestRenderInstructionAppendsSheetsOnlyWhenRelevant`, `TestSheetRulesLiveOnlyInFragment`, `TestLoadPromptsLoadsSheetsFragment` |
| 9router không hồi quy | `src/agent/eino/provider_test.go` | `TestBuildProviderAgentsKeepsNineRouterModel`, `TestBuildProviderAgentsSkipsProviderWithoutKey` |
| Luật mới | `prompts/chat_system_sheets.md` (15 luật), `prompts/chat_system.md` (3 luật chung) | trích dẫn `[^n]` ổn định, tôn trọng định dạng yêu cầu, hỏi lại thì trả lời ngắn |
| Tài liệu | `AGENTS.md`, `RAG_RESEARCH.md` (§6.20, §6.21, nguồn 34-37) | — |

Lệch so với kế hoạch (đã chốt trong lúc làm):

1. Bộ dò bảng tính quét **4 message gần nhất của mọi role** thay vì 2 lượt user — một câu trả lời của
   assistant nhắc sheet cũng là tín hiệu cho câu nối tiếp, và test “history chỉ có assistant” đòi đúng vậy.
2. Thêm marker `"bảng "` (có dấu cách) — thiếu nó thì “Bảng lương tháng 9 có bao nhiêu nhân viên?”
   (không có chữ Excel/sheet) sẽ không nạp fragment.
3. Bỏ luôn 3 câu chỉ dấu “xem phần Quy tắc riêng: bảng tính” khỏi prompt chung — để prompt gốc **0**
   từ vựng sheet, đúng yêu cầu “chỉ khi liên quan mới đẩy” và làm test tách file có nghĩa.
4. `chat_system_sheets.md` thành 15 luật (12 dự kiến + 3 tách rõ: ngoại lệ trần 8 lượt, không kết luận
   “không có số liệu” từ mảnh vỡ cột, dùng đúng ID file được gọi tên).

