package port

import (
	"context"

	"knowledge_ingestion/src/controller/dtos"
)

// ISearcher là năng lực search mà agent cung cấp và chat tiêu thụ. API gọi
// thẳng interface này.
type ISearcher interface {
	Search(ctx context.Context, dto *dtos.SearchRequest) dtos.Result[*dtos.SearchResponse]
}

// IChatter là năng lực chat agent cung cấp. API gọi thẳng interface này.
type IChatter interface {
	Chat(ctx context.Context, dto *dtos.ChatRequest) dtos.Result[*dtos.ChatResponse]
}

// IUserResolver lấy user_id của người gọi từ ctx — agent chỉ cần method này,
// không cần biết middleware auth.
type IUserResolver interface {
	UserID(ctx context.Context) (int64, bool)
}
