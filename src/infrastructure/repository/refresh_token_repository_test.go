package repository

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"knowledge_ingestion/src/common/utils"
	"knowledge_ingestion/src/config"
	"knowledge_ingestion/src/domain"
	"knowledge_ingestion/src/infrastructure/postgres"

	"gorm.io/gorm"
)

// newRefreshTokenTestDB mở DB thật từ config; không reachable thì skip để
// test chạy được cả trên máy không bật Docker.
func newRefreshTokenTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	path := os.Getenv("APP_CONFIG_PATH")
	if path == "" {
		path = filepath.Join("..", "..", "..", "configs", "config.json")
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Skipf("config không đọc được: %v", err)
	}
	conn, err := postgres.NewConnection(cfg)
	if err != nil {
		t.Skipf("postgres không reachable: %v", err)
	}
	return conn.GetDB()
}

// seedRefreshTokenTestUser tạo user tạm + dọn sạch sau test (xoá cứng để
// không để lại rác trong DB dev).
func seedRefreshTokenTestUser(t *testing.T, db *gorm.DB) int64 {
	t.Helper()
	username := fmt.Sprintf("rt_test_%d", time.Now().UnixNano())
	user := &domain.User{UserName: username, Email: username + "@example.com", Active: true, Password: "x"}
	if err := NewUserRepository(db).CreateUser(context.Background(), user); err != nil {
		t.Fatalf("tạo user tạm: %v", err)
	}
	t.Cleanup(func() {
		db.Unscoped().Where("user_id = ?", user.ID).Delete(&domain.RefreshToken{})
		db.Unscoped().Where("id = ?", user.ID).Delete(&domain.User{})
	})
	return user.ID
}

func TestRefreshTokenRepositoryXoayVongVaPhatHienReuse(t *testing.T) {
	db := newRefreshTokenTestDB(t)
	repo := NewRefreshTokenRepository(db)
	ctx := context.Background()
	userID := seedRefreshTokenTestUser(t, db)

	now := time.Now()
	familyID, err := utils.NewUUIDv7()
	if err != nil {
		t.Fatalf("uuid: %v", err)
	}
	_, hash1, err := utils.NewOpaqueToken()
	if err != nil {
		t.Fatalf("token: %v", err)
	}
	first := &domain.RefreshToken{
		UserID:          userID,
		FamilyID:        familyID,
		TokenHash:       hash1,
		ExpiresAt:       now.Add(24 * time.Hour),
		FamilyExpiresAt: now.Add(72 * time.Hour),
	}
	if err := repo.Create(ctx, first); err != nil {
		t.Fatalf("Create: %v", err)
	}

	// 1) Xoay lần đầu: token cũ bị đánh dấu, token mới cùng family.
	_, hash2, err := utils.NewOpaqueToken()
	if err != nil {
		t.Fatalf("token 2: %v", err)
	}
	old, err := repo.Rotate(ctx, hash1, &domain.RefreshToken{TokenHash: hash2, ExpiresAt: now.Add(24 * time.Hour)})
	if err != nil {
		t.Fatalf("Rotate lần đầu: %v", err)
	}
	if old.UserID != userID || old.FamilyID != familyID {
		t.Fatalf("Rotate trả row cũ sai: %+v", old)
	}

	var rotated domain.RefreshToken
	if err := db.First(&rotated, first.ID).Error; err != nil {
		t.Fatalf("đọc lại token cũ: %v", err)
	}
	if rotated.RotatedAt == nil {
		t.Fatal("token cũ chưa được đánh dấu rotated_at")
	}

	var second domain.RefreshToken
	if err := db.Where("token_hash = ?", hash2).First(&second).Error; err != nil {
		t.Fatalf("token mới chưa được chèn: %v", err)
	}
	if second.FamilyID != familyID || second.ExpiresAt.Sub(now) < 23*time.Hour {
		t.Fatalf("token mới sai family/hạn: %+v", second)
	}

	// 2) Dùng lại token đã xoay → reuse, service sẽ thu hồi family.
	_, err = repo.Rotate(ctx, hash1, &domain.RefreshToken{TokenHash: "dummy-1", ExpiresAt: now.Add(time.Hour)})
	if !errors.Is(err, domain.ErrRefreshReuse) {
		t.Fatalf("muốn ErrRefreshReuse, được %v", err)
	}

	// 3) Thu hồi family bằng token mới nhất.
	n, err := repo.RevokeFamilyByHash(ctx, hash2, domain.RefreshRevokeReuse)
	if err != nil {
		t.Fatalf("RevokeFamilyByHash: %v", err)
	}
	if n < 1 {
		t.Fatalf("phải thu hồi ít nhất 1 row, được %d", n)
	}

	var revoked domain.RefreshToken
	if err := db.Where("token_hash = ?", hash2).First(&revoked).Error; err != nil {
		t.Fatalf("đọc token mới: %v", err)
	}
	if revoked.RevokedAt == nil || revoked.RevokeReason != domain.RefreshRevokeReuse {
		t.Fatalf("token mới chưa bị thu hồi đúng lý do: %+v", revoked)
	}

	// 4) Token đã thu hồi không xoay được nữa.
	_, err = repo.Rotate(ctx, hash2, &domain.RefreshToken{TokenHash: "dummy-2", ExpiresAt: now.Add(time.Hour)})
	if !errors.Is(err, domain.ErrRefreshRevoked) {
		t.Fatalf("muốn ErrRefreshRevoked, được %v", err)
	}

	// 5) Logout idempotent: hash lạ trả 0, không lỗi.
	if n, err := repo.RevokeFamilyByHash(ctx, "khong-ton-tai", domain.RefreshRevokeLogout); err != nil || n != 0 {
		t.Fatalf("logout hash lạ phải (0,nil), được (%d,%v)", n, err)
	}
}

func TestRefreshTokenRepositoryTuChoiTokenHetHan(t *testing.T) {
	db := newRefreshTokenTestDB(t)
	repo := NewRefreshTokenRepository(db)
	ctx := context.Background()
	userID := seedRefreshTokenTestUser(t, db)

	now := time.Now()
	familyID, err := utils.NewUUIDv7()
	if err != nil {
		t.Fatalf("uuid: %v", err)
	}

	// Token hết hạn nhưng family còn sống.
	_, expiredHash, err := utils.NewOpaqueToken()
	if err != nil {
		t.Fatalf("token: %v", err)
	}
	expired := &domain.RefreshToken{
		UserID:          userID,
		FamilyID:        familyID,
		TokenHash:       expiredHash,
		ExpiresAt:       now.Add(-2 * time.Hour),
		FamilyExpiresAt: now.Add(24 * time.Hour),
	}
	if err := repo.Create(ctx, expired); err != nil {
		t.Fatalf("Create: %v", err)
	}
	_, err = repo.Rotate(ctx, expiredHash, &domain.RefreshToken{TokenHash: "dummy", ExpiresAt: now.Add(time.Hour)})
	if !errors.Is(err, domain.ErrRefreshExpired) {
		t.Fatalf("muốn ErrRefreshExpired, được %v", err)
	}

	// Family quá hạn tuyệt đối dù token còn hạn.
	_, familyHash, err := utils.NewOpaqueToken()
	if err != nil {
		t.Fatalf("token: %v", err)
	}
	deadFamily := &domain.RefreshToken{
		UserID:          userID,
		FamilyID:        familyID,
		TokenHash:       familyHash,
		ExpiresAt:       now.Add(24 * time.Hour),
		FamilyExpiresAt: now.Add(-time.Minute),
	}
	if err := repo.Create(ctx, deadFamily); err != nil {
		t.Fatalf("Create: %v", err)
	}
	_, err = repo.Rotate(ctx, familyHash, &domain.RefreshToken{TokenHash: "dummy-2", ExpiresAt: now.Add(time.Hour)})
	if !errors.Is(err, domain.ErrRefreshExpired) {
		t.Fatalf("muốn ErrRefreshExpired cho family quá hạn, được %v", err)
	}

	// Janitor xoá cứng token đã chết quá 1 giờ.
	if _, err := repo.DeleteExpired(ctx, now.Add(-time.Hour)); err != nil {
		t.Fatalf("DeleteExpired: %v", err)
	}
	var count int64
	if err := db.Model(&domain.RefreshToken{}).Where("token_hash = ?", expiredHash).Count(&count).Error; err != nil {
		t.Fatalf("đếm token: %v", err)
	}
	if count != 0 {
		t.Fatalf("token hết hạn chưa bị xoá cứng, còn %d row", count)
	}
}
