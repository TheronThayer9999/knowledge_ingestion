package loader

import (
	"testing"

	"knowledge_ingestion/src/config"
	"knowledge_ingestion/src/controller/routers"

	"go.uber.org/fx"
)

// fakeConfig là config rỗng cho test — ValidateApp KHÔNG chạy constructor nên
// giá trị không quan trọng, chỉ cần đủ method cho interface IConfig.
type fakeConfig struct{}

func (fakeConfig) GetApp() config.AppConfig             { return config.AppConfig{} }
func (fakeConfig) GetDatabase() config.DBConfig         { return config.DBConfig{} }
func (fakeConfig) GetStorage() config.S3Config          { return config.S3Config{} }
func (fakeConfig) GetEmbedding() config.EmbeddingConfig { return config.EmbeddingConfig{} }
func (fakeConfig) GetLLMs() config.LLMsConfig           { return config.LLMsConfig{} }
func (fakeConfig) GetQdrant() config.QdrantConfig       { return config.QdrantConfig{} }
func (fakeConfig) GetOCR() config.OCRConfig             { return config.OCRConfig{} }
func (fakeConfig) GetRedis() config.RedisConfig         { return config.RedisConfig{} }
func (fakeConfig) GetRabbitMQ() config.RabbitMQConfig   { return config.RabbitMQConfig{} }
func (fakeConfig) GetJWT() config.JWTConfig             { return config.JWTConfig{} }
func (fakeConfig) GetCORS() config.CorsConfig           { return config.CorsConfig{} }
func (fakeConfig) GetWorker() config.WorkerConfig       { return config.WorkerConfig{} }
func (fakeConfig) GetAgent() config.AgentConfig         { return config.AgentConfig{} }
func (fakeConfig) GetVLM() config.VLMConfig             { return config.VLMConfig{} }
func (fakeConfig) GetChatCrypto() config.ChatCryptoConfig {
	return config.ChatCryptoConfig{}
}

// TestFxGraph kiểm tra đồ thị DI của API — thiếu một vai hẹp (vd service BYOK
// chưa bind sang interface) thì app CHẾT lúc boot, mà lúc boot thì mất cả DB
// và cả key provider. Test này bắt lỗi đó trước khi chạy thật.
//
// Invoke router là cố ý: phần lớn graph chỉ được chạm tới khi dựng router,
// không invoke thì fx không bắt được binding hỏng trong nhánh đó.
func TestFxGraph(t *testing.T) {
	err := fx.ValidateApp(
		fx.Provide(func() config.IConfig { return fakeConfig{} }),
		fx.Options(Load()...),
		fx.Invoke(func(*routers.Router) {}),
	)
	if err != nil {
		t.Fatalf("đồ thị DI không hợp lệ: %v", err)
	}
}
