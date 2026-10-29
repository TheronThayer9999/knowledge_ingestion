package services

import (
	"context"

	"knowledge_ingestion/src/controller/dtos"
	"knowledge_ingestion/src/domain"
)

// ILLMUsageAdminService báo cáo chi phí LLM theo tài khoản — chỉ admin gọi.
//
// Báo cáo (không chặn): mục tiêu là quản lý/thống kê, không phải quota.
// Người dùng vẫn chat bình thường; admin đọc số liệu để biết acc nào đang
// tốn, rồi quyết định có siết hay không.
type ILLMUsageAdminService interface {
	List(ctx context.Context, q dtos.LLMUsageListQuery) dtos.Result[*dtos.LLMUsageListResponse]
}

type llmUsageAdminService struct {
	repo domain.ILLMUsageRepository
}

func NewLLMUsageAdminService(repo domain.ILLMUsageRepository) ILLMUsageAdminService {
	return &llmUsageAdminService{repo: repo}
}

// List trả các dòng tổng hợp usage theo bộ lọc kèm tổng — admin không phải
// cộng tay để biết 1 ngày tốn bao nhiêu.
func (s *llmUsageAdminService) List(ctx context.Context, q dtos.LLMUsageListQuery) dtos.Result[*dtos.LLMUsageListResponse] {
	rows, err := s.repo.List(ctx, domain.LLMUsageFilter{
		From:   q.From,
		To:     q.To,
		UserID: q.UserID,
		Site:   q.Site,
		Limit:  q.Limit,
		Offset: q.Offset,
	})
	if err != nil {
		return dtos.Fail[*dtos.LLMUsageListResponse](err)
	}
	return dtos.Ok(&dtos.LLMUsageListResponse{
		Items: usageItems(rows),
		Total: usageTotals(rows),
	})
}

// usageItems map dòng repo → DTO cho FE.
func usageItems(rows []*domain.LLMUsageRow) []dtos.LLMUsageDailyResponse {
	items := make([]dtos.LLMUsageDailyResponse, 0, len(rows))
	for _, r := range rows {
		if r == nil {
			continue
		}
		items = append(items, dtos.LLMUsageDailyResponse{
			UserID:           r.UserID,
			UserName:         r.UserName,
			Email:            r.Email,
			Day:              r.Day,
			Site:             r.Site,
			Provider:         r.Provider,
			Model:            r.Model,
			Calls:            r.Calls,
			PromptTokens:     r.PromptTokens,
			CompletionTokens: r.CompletionTokens,
			TotalTokens:      r.TotalTokens,
			Errors:           r.Errors,
		})
	}
	return items
}

// usageTotals cộng tổng số lượt gọi + token của các dòng trả về.
func usageTotals(rows []*domain.LLMUsageRow) dtos.LLMUsageTotals {
	var t dtos.LLMUsageTotals
	for _, r := range rows {
		if r == nil {
			continue
		}
		t.Calls += r.Calls
		t.PromptTokens += r.PromptTokens
		t.CompletionTokens += r.CompletionTokens
		t.TotalTokens += r.TotalTokens
		t.Errors += r.Errors
	}
	return t
}
