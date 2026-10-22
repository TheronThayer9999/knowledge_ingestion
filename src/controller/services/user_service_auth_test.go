package services

import (
	"context"
	"testing"
	"time"

	commonerrors "knowledge_ingestion/src/common/errors"
	"knowledge_ingestion/src/common/utils"
	"knowledge_ingestion/src/config"
	"knowledge_ingestion/src/controller/dtos"
	"knowledge_ingestion/src/controller/middlewares"
	"knowledge_ingestion/src/domain"

	"golang.org/x/crypto/bcrypt"
)

// fakeIConfig chỉ cần GetJWT — nhúng interface để khỏi stub 12 method.
type fakeIConfig struct{ config.IConfig }

func (fakeIConfig) GetJWT() config.JWTConfig {
	return config.JWTConfig{
		Secret:              "test-secret",
		ExpiryMinutes:       15,
		RefreshExpiryDays:   30,
		RefreshAbsoluteDays: 90,
	}
}

type fakeAuth struct{ middlewares.IAuthMiddleware }

func (fakeAuth) IssueToken(int64, string, time.Time) (string, time.Time, error) {
	return "access-token", time.Now().Add(15 * time.Minute), nil
}

type fakeUserRepo struct {
	domain.IUserRepositoryImpl
	user            *domain.User
	err             error
	updatedPassword int64
}

func (f *fakeUserRepo) GetUserByUsername(context.Context, string) (*domain.User, error) {
	return f.user, f.err
}

func (f *fakeUserRepo) GetUserById(context.Context, int64) (*domain.User, error) {
	return f.user, f.err
}

func (f *fakeUserRepo) TouchLastLogin(context.Context, int64, time.Time) error { return nil }

func (f *fakeUserRepo) UpdatePassword(_ context.Context, id int64, hash string, at time.Time) error {
	f.updatedPassword = id
	if f.user != nil {
		f.user.Password = hash
		f.user.PasswordChangedAt = at
	}
	return nil
}

type fakeRefreshRepo struct {
	domain.IRefreshTokenRepository
	created         []*domain.RefreshToken
	rotatedHash     string
	rotateOld       *domain.RefreshToken
	rotateErr       error
	revokedFamilies []string
	revokedUsers    []int64
}

func (f *fakeRefreshRepo) Create(_ context.Context, token *domain.RefreshToken) error {
	token.ID = int64(len(f.created) + 1)
	f.created = append(f.created, token)
	return nil
}

func (f *fakeRefreshRepo) Rotate(_ context.Context, hash string, next *domain.RefreshToken) (*domain.RefreshToken, error) {
	f.rotatedHash = hash
	if f.rotateErr != nil {
		return f.rotateOld, f.rotateErr
	}
	next.UserID = f.rotateOld.UserID
	next.FamilyID = f.rotateOld.FamilyID
	next.FamilyExpiresAt = f.rotateOld.FamilyExpiresAt
	return f.rotateOld, nil
}

func (f *fakeRefreshRepo) RevokeFamilyByHash(_ context.Context, hash, reason string) (int64, error) {
	f.revokedFamilies = append(f.revokedFamilies, hash+":"+reason)
	return 1, nil
}

func (f *fakeRefreshRepo) RevokeAllByUser(_ context.Context, userID int64, _ string) (int64, error) {
	f.revokedUsers = append(f.revokedUsers, userID)
	return 1, nil
}

func activeUser(t *testing.T, password string) *domain.User {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("bcrypt: %v", err)
	}
	return &domain.User{
		BaseModel: domain.BaseModel{ID: 7},
		UserName:  "an",
		Password:  string(hash),
		Active:    true,
	}
}

func newAuthTestService(t *testing.T, users *fakeUserRepo, refresh *fakeRefreshRepo) IUserService {
	t.Helper()
	return NewUserService(fakeIConfig{}, fakeAuth{}, users, refresh)
}

func TestLoginPhatRefreshTokenChiLuuHash(t *testing.T) {
	users := &fakeUserRepo{user: activeUser(t, "secret123")}
	refresh := &fakeRefreshRepo{}
	svc := newAuthTestService(t, users, refresh)

	res := svc.Login(context.Background(), &dtos.LoginUserRequest{Username: "an", Password: "secret123"}, dtos.SessionMeta{IP: "1.2.3.4"})
	if res.Err != nil {
		t.Fatalf("Login lỗi: %v", res.Err)
	}
	if res.Data.Token != "access-token" || res.Data.RefreshToken == "" {
		t.Fatalf("thiếu token: %+v", res.Data)
	}
	if len(refresh.created) != 1 {
		t.Fatalf("phải chèn đúng 1 refresh token, được %d", len(refresh.created))
	}
	row := refresh.created[0]
	if row.UserID != 7 || row.FamilyID == "" || row.TokenHash == "" {
		t.Fatalf("row thiếu dữ liệu: %+v", row)
	}
	if row.TokenHash == res.Data.RefreshToken {
		t.Fatal("DB phải lưu hash, không được lưu token gốc")
	}
	if row.TokenHash != utils.HashToken(res.Data.RefreshToken) {
		t.Fatal("hash lưu DB không khớp token trả client")
	}
	if row.IP != "1.2.3.4" {
		t.Fatalf("thiếu IP audit: %+v", row)
	}
	if !row.ExpiresAt.After(time.Now().Add(29*24*time.Hour)) || !row.FamilyExpiresAt.After(time.Now().Add(89*24*time.Hour)) {
		t.Fatalf("hạn sai: expires=%v family=%v", row.ExpiresAt, row.FamilyExpiresAt)
	}
}

func TestRefreshXoayTokenVaTraCapMoi(t *testing.T) {
	old := &domain.RefreshToken{
		BaseModel:       domain.BaseModel{ID: 3},
		UserID:          7,
		FamilyID:        "fam-1",
		FamilyExpiresAt: time.Now().Add(60 * 24 * time.Hour),
	}
	users := &fakeUserRepo{user: activeUser(t, "secret123")}
	refresh := &fakeRefreshRepo{rotateOld: old}
	svc := newAuthTestService(t, users, refresh)

	res := svc.Refresh(context.Background(), &dtos.RefreshTokenRequest{RefreshToken: "old-token"}, dtos.SessionMeta{})
	if res.Err != nil {
		t.Fatalf("Refresh lỗi: %v", res.Err)
	}
	if res.Data.RefreshToken == "" || res.Data.RefreshToken == "old-token" {
		t.Fatal("phải trả refresh token mới, khác token cũ")
	}
	if res.Data.Token != "access-token" {
		t.Fatalf("access token sai: %q", res.Data.Token)
	}
	if refresh.rotatedHash != utils.HashToken("old-token") {
		t.Fatal("phải tra DB bằng hash của token client gửi")
	}
	if res.Data.RefreshExpiresAt.After(old.FamilyExpiresAt) {
		t.Fatal("token mới không được vượt hạn tuyệt đối của family")
	}
}

func TestRefreshPhatHienReuseThuHoiCaFamily(t *testing.T) {
	refresh := &fakeRefreshRepo{
		rotateOld: &domain.RefreshToken{UserID: 7, FamilyID: "fam-1"},
		rotateErr: domain.ErrRefreshReuse,
	}
	svc := newAuthTestService(t, &fakeUserRepo{}, refresh)

	res := svc.Refresh(context.Background(), &dtos.RefreshTokenRequest{RefreshToken: "stolen"}, dtos.SessionMeta{})
	assertUnauthorized(t, res.Err)
	if len(refresh.revokedFamilies) != 1 {
		t.Fatalf("reuse phải thu hồi family, được %v", refresh.revokedFamilies)
	}
	want := utils.HashToken("stolen") + ":" + domain.RefreshRevokeReuse
	if refresh.revokedFamilies[0] != want {
		t.Fatalf("thu hồi sai: %s != %s", refresh.revokedFamilies[0], want)
	}
}

func TestRefreshTokenKhongTonTaiTra401(t *testing.T) {
	refresh := &fakeRefreshRepo{rotateErr: domain.ErrRefreshNotFound}
	svc := newAuthTestService(t, &fakeUserRepo{}, refresh)

	res := svc.Refresh(context.Background(), &dtos.RefreshTokenRequest{RefreshToken: "unknown"}, dtos.SessionMeta{})
	assertUnauthorized(t, res.Err)
}

func TestRefreshUserBiKhoaThuHoiHetVa403(t *testing.T) {
	user := activeUser(t, "secret123")
	user.Active = false
	refresh := &fakeRefreshRepo{rotateOld: &domain.RefreshToken{UserID: 7, FamilyID: "fam-1"}}
	svc := newAuthTestService(t, &fakeUserRepo{user: user}, refresh)

	res := svc.Refresh(context.Background(), &dtos.RefreshTokenRequest{RefreshToken: "tok"}, dtos.SessionMeta{})
	if res.Err == nil {
		t.Fatal("user bị khóa phải lỗi")
	}
	if got := commonerrors.From(res.Err); got == nil || got.HttpCode != 403 {
		t.Fatalf("muốn 403, được %+v", got)
	}
	if len(refresh.revokedUsers) != 1 || refresh.revokedUsers[0] != 7 {
		t.Fatalf("phải thu hồi mọi token của user, được %v", refresh.revokedUsers)
	}
}

func TestLogoutThuHoiFamily(t *testing.T) {
	refresh := &fakeRefreshRepo{}
	svc := newAuthTestService(t, &fakeUserRepo{}, refresh)

	res := svc.Logout(context.Background(), &dtos.LogoutRequest{RefreshToken: "tok"})
	if res.Err != nil {
		t.Fatalf("Logout lỗi: %v", res.Err)
	}
	if res.Data.Revoked != 1 {
		t.Fatalf("Revoked = %d, muốn 1", res.Data.Revoked)
	}
	want := utils.HashToken("tok") + ":" + domain.RefreshRevokeLogout
	if len(refresh.revokedFamilies) != 1 || refresh.revokedFamilies[0] != want {
		t.Fatalf("logout sai: %v", refresh.revokedFamilies)
	}
}

func TestDoiMatKhauThuHoiMoiPhien(t *testing.T) {
	users := &fakeUserRepo{user: activeUser(t, "secret123")}
	refresh := &fakeRefreshRepo{}
	svc := newAuthTestService(t, users, refresh)

	res := svc.ChangePassword(context.Background(), 7, &dtos.ChangePasswordRequest{
		OldPassword: "secret123",
		NewPassword: "newsecret456",
	})
	if res.Err != nil {
		t.Fatalf("ChangePassword lỗi: %v", res.Err)
	}
	if users.updatedPassword != 7 {
		t.Fatal("chưa cập nhật mật khẩu")
	}
	if len(refresh.revokedUsers) != 1 || refresh.revokedUsers[0] != 7 {
		t.Fatalf("phải thu hồi refresh token sau đổi pass, được %v", refresh.revokedUsers)
	}
}

func assertUnauthorized(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("muốn 401, được nil")
	}
	got := commonerrors.From(err)
	if got == nil || got.HttpCode != 401 {
		t.Fatalf("muốn 401, được %+v", got)
	}
}
