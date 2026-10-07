package rabbitmq

import (
	"fmt"
	"knowledge_ingestion/src/common/logs"
	"knowledge_ingestion/src/config"
	"net"
	"net/url"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

// IConnection giữ 1 AMQP connection dùng chung cả app lifetime —
// Connection thread-safe, còn Channel thì KHÔNG nên mỗi goroutine mở
// channel riêng qua OpenChannel rồi tự đóng.
type IConnection interface {
	// OpenChannel mở channel mới cho 1 goroutine (relay publish hoặc
	// consumer). Caller đóng khi xong.
	OpenChannel() (*amqp.Channel, error)
	// Exchange trả tên exchange relay publish event vào.
	Exchange() string
	// Queue trả hàng đợi chính consumer đọc.
	Queue() string
	Close() error
}

type connection struct {
	conn     *amqp.Connection
	exchange string
	queue    string
}

func NewConnection(cfg config.IConfig) (IConnection, error) {
	r := cfg.GetRabbitMQ()
	endpoint := fmt.Sprintf("amqp://%s:%s@%s:%d/%s",
		url.QueryEscape(r.User), url.QueryEscape(r.Password), r.Host, r.Port, url.PathEscape(vhostPath(r.Vhost)))

	// Dial có timeout 10s — fail-fast giống postgres/S3 để không boot app
	// trong trạng thái relay mù. amqp091-go không nhận context nên set
	// deadline ở Dial của net.
	conn, err := amqp.DialConfig(endpoint, amqp.Config{
		Dial: func(network, addr string) (net.Conn, error) {
			return net.DialTimeout(network, addr, 10*time.Second)
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to dial rabbitmq %s:%d: %w", r.Host, r.Port, err)
	}

	c := &connection{conn: conn, exchange: r.Exchange, queue: r.Queue}
	if err := c.declareTopology(); err != nil {
		_ = conn.Close()
		return nil, err
	}

	logs.Infow("rabbitmq connected", "host", r.Host, "port", r.Port, "vhost", r.Vhost,
		"exchange", r.Exchange, "queue", r.Queue)
	return c, nil
}

// vhostPath đổi vhost thành path của AMQP URL — vhost "/" là chuỗi rỗng
// sau host (amqp://host:port/), vhost khác thì "/ten".
func vhostPath(vhost string) string {
	if vhost == "" || vhost == "/" {
		return ""
	}
	return vhost
}

// declareTopology dựng exchange topic durable + queue durable + bind tất cả
// routing key ("#") — chạy 1 lần lúc boot nên worker/relay khỏi lo topology
// chưa có. Queue sau này tách theo event_type thì bind thêm, không sửa đây.
func (c *connection) declareTopology() error {
	ch, err := c.conn.Channel()
	if err != nil {
		return fmt.Errorf("rabbitmq open channel: %w", err)
	}
	defer func() { _ = ch.Close() }()

	if err := ch.ExchangeDeclare(c.exchange, "topic", true, false, false, false, nil); err != nil {
		return fmt.Errorf("rabbitmq declare exchange %s: %w", c.exchange, err)
	}
	if _, err := ch.QueueDeclare(c.queue, true, false, false, false, nil); err != nil {
		return fmt.Errorf("rabbitmq declare queue %s: %w", c.queue, err)
	}
	if err := ch.QueueBind(c.queue, "#", c.exchange, false, nil); err != nil {
		return fmt.Errorf("rabbitmq bind queue %s: %w", c.queue, err)
	}
	return nil
}

func (c *connection) OpenChannel() (*amqp.Channel, error) {
	ch, err := c.conn.Channel()
	if err != nil {
		return nil, fmt.Errorf("rabbitmq open channel: %w", err)
	}
	return ch, nil
}

func (c *connection) Exchange() string { return c.exchange }

func (c *connection) Queue() string { return c.queue }

func (c *connection) Close() error { return c.conn.Close() }
