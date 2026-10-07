package domain

type Article struct {
	BaseModel

	URL         string `json:"url" gorm:"not null"`
	Name        string `json:"name" gorm:"not null"`
	ContentType string `json:"content_type" gorm:"not null"`
	Description string `json:"description,omitempty"`

	CategoryID int64 `json:"category_id" gorm:"not null;index"`
}

func (Article) TableName() string {
	return "articles"
}
