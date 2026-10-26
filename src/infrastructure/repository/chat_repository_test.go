package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"knowledge_ingestion/src/domain"
)

// TestConversationRepositoryKeysetAndOwner là test DB thật (bỏ qua khi Docker
// tắt) cho 2 thứ fake không chứng minh được: mệnh đề keyset row-value của List
// và các UPDATE có điều kiện (đúng chủ sở hữu / title đang rỗng).
func TestConversationRepositoryKeysetAndOwner(t *testing.T) {
	db := newPageIssuesTestDB(t) // harness chung: mở config.local.json → config.json, skip khi không reachable
	repo := NewConversationRepository(db)
	ctx := context.Background()

	// user_id lấy theo nano để không va phải dữ liệu thật của máy dev.
	userID := time.Now().UnixNano()
	foreign := userID + 1
	now := time.Now().Truncate(time.Millisecond)
	var created []*domain.Conversation
	t.Cleanup(func() {
		ids := make([]int64, 0, len(created))
		for _, conv := range created {
			ids = append(ids, conv.ID)
		}
		db.Unscoped().Where("id IN ?", ids).Delete(&domain.Conversation{})
	})

	newConv := func(title string, at time.Time, userID int64) *domain.Conversation {
		conv := &domain.Conversation{
			UserID:           userID,
			Title:            title,
			Provider:         "ollama",
			ScopeCategoryIDs: "[]",
			LastMessageAt:    at,
		}
		if err := repo.Create(ctx, conv); err != nil {
			t.Fatalf("Create: %v", err)
		}
		created = append(created, conv)
		return conv
	}

	// Ba phiên của user, mốc thời gian tăng dần — thứ tự sidebar phải NGƯỢC
	// (mới nhất trước) và id phá hoà khi two phiên cùng last_message_at.
	c1 := newConv("phiên một", now, userID)
	c2 := newConv("", now.Add(time.Minute), userID)
	c3 := newConv("phiên ba", now.Add(2*time.Minute), userID)
	newConv("phiên của người khác", now.Add(3*time.Minute), foreign)

	listIDs := func(beforeID int64, limit int) string {
		out, err := repo.List(ctx, userID, beforeID, limit)
		if err != nil {
			t.Fatalf("List(before=%d, limit=%d): %v", beforeID, limit, err)
		}
		parts := make([]string, 0, len(out))
		for _, conv := range out {
			parts = append(parts, fmt.Sprint(conv.ID))
		}
		return strings.Join(parts, ",")
	}
	joinIDs := func(ids ...*domain.Conversation) string {
		parts := make([]string, 0, len(ids))
		for _, id := range ids {
			parts = append(parts, fmt.Sprint(id.ID))
		}
		return strings.Join(parts, ",")
	}

	if got := listIDs(0, 2); got != joinIDs(c3, c2) {
		t.Errorf("List trang 1 = %s, want %s (mới nhất trước, không lọt phiên người khác)", got, joinIDs(c3, c2))
	}
	// Con trỏ = c2 → chỉ còn c1: keyset so sánh (last_message_at, id) chứ không
	// đếm OFFSET, nên trang 2 không lặp và không sót dù trang 1 bị cắt giữa dòng.
	if got := listIDs(c2.ID, 2); got != joinIDs(c1) {
		t.Errorf("List theo con trỏ = %s, want %s", got, joinIDs(c1))
	}
	if out, err := repo.List(ctx, foreign, 0, 10); err != nil || len(out) != 1 {
		t.Errorf("List user khác = %d dòng, want đúng phiên của họ (err=%v)", len(out), err)
	}

	// SetTitleIfEmpty: chỉ điền khi title rỗng — phiên đã có tên không được
	// lượt sau đổi tên (đó là việc của PATCH /rename).
	if err := repo.SetTitleIfEmpty(ctx, c1.ID, "title mới từ lượt hai"); err != nil {
		t.Fatalf("SetTitleIfEmpty: %v", err)
	}
	if err := repo.SetTitleIfEmpty(ctx, c2.ID, "phiên hai"); err != nil {
		t.Fatalf("SetTitleIfEmpty: %v", err)
	}
	if got, err := repo.GetByID(ctx, c1.ID, userID); err != nil || got.Title != "phiên một" {
		t.Errorf("title bị ghi đè = %q (err=%v), want giữ nguyên", got.Title, err)
	}
	if got, err := repo.GetByID(ctx, c2.ID, userID); err != nil || got.Title != "phiên hai" {
		t.Errorf("title của phiên rỗng = %q (err=%v), want được điền", got.Title, err)
	}

	// TouchMessageTime kéo phiên cũ lên đầu — đây là thứ sidebar sắp xếp.
	later := now.Add(10 * time.Minute)
	if err := repo.TouchMessageTime(ctx, c1.ID, later); err != nil {
		t.Fatalf("TouchMessageTime: %v", err)
	}
	if got, err := repo.GetByID(ctx, c1.ID, userID); err != nil || !got.LastMessageAt.Equal(later) {
		t.Errorf("last_message_at = %v (err=%v), want %v", got.LastMessageAt, err, later)
	}
	if got := listIDs(0, 2); got != joinIDs(c1, c3) {
		t.Errorf("List sau touch = %s, want %s", got, joinIDs(c1, c3))
	}

	// TouchScope ghi jsonb hint — rỗng phải là "[]" chứ không phải '' (cột
	// jsonb NOT NULL từ chối chuỗi rỗng). Postgres TỰ CHUẨN HOÁ jsonb
	// (`[3,9]` lưu về là `[3, 9]` có khoảng trắng), nên hợp đồng ở đây là "giải
	// mã ra đúng danh sách id", không phải chuỗi byte-đ-byte.
	if err := repo.TouchScope(ctx, c3.ID, "[3,9]"); err != nil {
		t.Fatalf("TouchScope: %v", err)
	}
	got, err := repo.GetByID(ctx, c3.ID, userID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	var scope []int64
	if err := json.Unmarshal([]byte(got.ScopeCategoryIDs), &scope); err != nil {
		t.Fatalf("scope %q không phải jsonb hợp lệ: %v", got.ScopeCategoryIDs, err)
	}
	if len(scope) != 2 || scope[0] != 3 || scope[1] != 9 {
		t.Errorf("scope = %v, want [3,9]", scope)
	}

	// Xoá phải khớp CẢ id lẫn user_id: id của người khác → 0 dòng, không xoá
	// hộ; GetByID sau đó là ErrNotFound (mềm, nên bản ghi vẫn còn để purge).
	if n, err := repo.Delete(ctx, c1.ID, foreign); err != nil || n != 0 {
		t.Errorf("Delete bởi người lạ = (%d,%v), want (0,nil)", n, err)
	}
	if n, err := repo.Delete(ctx, c1.ID, userID); err != nil || n != 1 {
		t.Fatalf("Delete chủ sở hữu = (%d,%v), want (1,nil)", n, err)
	}
	if _, err := repo.GetByID(ctx, c1.ID, userID); err != domain.ErrNotFound {
		t.Errorf("GetByID sau xoá = %v, want domain.ErrNotFound", err)
	}
}

// TestChatMessageRepositoryWindows khoá 2 hành vi chỉ có DB mới kiểm được:
// ListRecent cắt N message MỚI NHẤT rồi trả về theo thứ tự CŨ→MỚI (fake không
// có ORDER/LIMIT), và CountByConversationIDs gom theo từng phiên trong MỘT
// câu GROUP BY.
func TestChatMessageRepositoryWindows(t *testing.T) {
	db := newPageIssuesTestDB(t)
	convRepo := NewConversationRepository(db)
	repo := NewChatMessageRepository(db)
	ctx := context.Background()

	userID := time.Now().UnixNano()
	now := time.Now()
	conv := &domain.Conversation{UserID: userID, Title: "một phiên", ScopeCategoryIDs: "[]", LastMessageAt: now}
	if err := convRepo.Create(ctx, conv); err != nil {
		t.Fatalf("Create conversation: %v", err)
	}
	msgIDs := make([]int64, 0, 5)
	t.Cleanup(func() {
		db.Unscoped().Where("id IN ?", msgIDs).Delete(&domain.ChatMessage{})
		db.Unscoped().Where("id = ?", conv.ID).Delete(&domain.Conversation{})
	})

	// 5 message liên tiếp (hỏi/đáp ×2 + hỏi) — 1 lần Append như AppendTurn làm.
	// Message 2 mang MỘT MẢNG cites thật (dạng normalizeCitations trả ra) để khoá
	// chiều ghi–đọc jsonb object mà không phải chờ LLM kiếm được hit.
	const citesJSON = `[{"n":1,"article_id":8,"page_num":4,"chunk_index":2,"snippet":"Điều 13"}]`
	msgs := make([]*domain.ChatMessage, 0, 5)
	for i := range 5 {
		role := domain.MessageRoleUser
		cites := "[]"
		if i%2 == 1 {
			role = domain.MessageRoleAssistant
		}
		if i == 1 {
			cites = citesJSON
		}
		msgs = append(msgs, &domain.ChatMessage{
			ConversationID:       conv.ID,
			Role:                 role,
			Content:              fmt.Sprintf("message %d", i+1),
			Cites:                cites,
			EffectiveCategoryIDs: "[3]",
		})
	}
	if err := repo.Append(ctx, msgs); err != nil {
		t.Fatalf("Append: %v", err)
	}
	for _, msg := range msgs {
		msgIDs = append(msgIDs, msg.ID)
	}

	// Cửa sổ 2 message cuối: phải là "message 4","message 5" theo thứ tự CŨ→MỚI.
	// Order("id ASC").Limit(2) thẳng sẽ trả "message 1","message 2" — ngữ cảnh
	// sai hoàn toàn mà fake không bao giờ phát hiện.
	recent, err := repo.ListRecent(ctx, conv.ID, 2)
	if err != nil {
		t.Fatalf("ListRecent: %v", err)
	}
	if len(recent) != 2 || recent[0].Content != "message 4" || recent[1].Content != "message 5" {
		got := make([]string, 0, len(recent))
		for _, msg := range recent {
			got = append(got, msg.Content)
		}
		t.Errorf("ListRecent = %v, want [message 4, message 5]", got)
	}
	// message 4 là đáp (i = 3), message 5 là hỏi tiếp (i = 4) — role phải về
	// đúng theo hàng, không phải theo vị trí trong cửa sổ.
	if recent[0].Role != domain.MessageRoleAssistant || recent[1].Role != domain.MessageRoleUser {
		t.Errorf("role = %q/%q, want assistant rồi user", recent[0].Role, recent[1].Role)
	}
	if recent[0].EffectiveCategoryIDs != "[3]" {
		t.Errorf("scope = %q, want [3] đọc nguyên vẹn từ jsonb", recent[0].EffectiveCategoryIDs)
	}

	// ListPage trả newest-first cho UI cuộn lên trên, mốc beforeID là id.
	page, err := repo.ListPage(ctx, conv.ID, recent[0].ID, 2)
	if err != nil {
		t.Fatalf("ListPage: %v", err)
	}
	if len(page) != 2 || page[0].Content != "message 3" || page[1].Content != "message 2" {
		got := make([]string, 0, len(page))
		for _, msg := range page {
			got = append(got, msg.Content)
		}
		t.Errorf("ListPage = %v, want [message 3, message 2]", got)
	}

	counts, err := repo.CountByConversationIDs(ctx, []int64{conv.ID, conv.ID + 1_000_000})
	if err != nil {
		t.Fatalf("CountByConversationIDs: %v", err)
	}
	if counts[conv.ID] != 5 {
		t.Errorf("count = %d, want 5", counts[conv.ID])
	}
	if _, ok := counts[conv.ID+1_000_000]; ok {
		t.Error("phiên không có message vẫn bị đếm — GROUP BY phải bỏ nó")
	}

	// Append danh sách rỗng là no-op, không được phát sinh INSERT hỏng.
	if err := repo.Append(ctx, nil); err != nil {
		t.Errorf("Append(nil) = %v, want nil", err)
	}

	// Đọc lại cites của message 2 và giải mã như mapper API vẫn làm: mảng object
	// jsonb phải về đủ n/article_id/snippet — mở lại phiên là chip [^n] sống lại.
	two, err := repo.ListRecent(ctx, conv.ID, 4)
	if err != nil || len(two) != 4 {
		t.Fatalf("ListRecent(4) = %d dòng err %v, want 4", len(two), err)
	}
	var got []struct {
		N         int    `json:"n"`
		ArticleID int64  `json:"article_id"`
		Snippet   string `json:"snippet"`
	}
	if err := json.Unmarshal([]byte(two[0].Cites), &got); err != nil {
		t.Fatalf("cites %q không parse được: %v", two[0].Cites, err)
	}
	if len(got) != 1 || got[0].N != 1 || got[0].ArticleID != 8 || got[0].Snippet != "Điều 13" {
		t.Errorf("cites đọc về = %+v, want [{1 8 ...Điều 13}]", got)
	}
	if two[3].Cites != "[]" {
		t.Errorf("cites của hàng không có nguồn = %q, want []", two[3].Cites)
	}
}
