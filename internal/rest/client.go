/*
ФАЙЛ: internal/rest/client.go

ОПИСАНИЕ:
Низкоуровневый REST-клиент SDK. Тонкий слой над http.Client, который:
  1. собирает URL (BaseURL + path + query);
  2. подписывает запрос (Signer);
  3. выполняет HTTP-вызов с дедлайном из ctx или Config.RequestTimeout;
  4. парсит обёртку OKX {code, msg, data};
  5. маппит ошибки в *okxerr.Error с правильной категорией;
  6. собирает rate-limit заголовки для возврата вызывающему коду.

ВАЖНО ПРО ИМПОРТЫ:
  - НЕ импортирует корневой okx-пакет (он импортирует rest), чтобы избежать
    import-cycle. Все нужные типы (Error/ErrorKind/Logger/Config) живут в
    internal/okxerr, internal/okxlog и в локальном Config.
*/

package rest

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/tonymontanov/go-okx/v2/internal/auth"
	"github.com/tonymontanov/go-okx/v2/internal/codec"
	"github.com/tonymontanov/go-okx/v2/internal/okxerr"
	"github.com/tonymontanov/go-okx/v2/internal/okxlog"
)

// rateLimitHeaders — заголовки OKX, которые мы возвращаем вызывающему коду.
var rateLimitHeaders = []string{
	"ratelimit-limit",
	"ratelimit-remaining",
	"ratelimit-reset",
	"x-ratelimit-limit",
	"x-ratelimit-remaining",
	"x-ratelimit-reset",
}

// Config — параметры REST-транспорта. Заполняется из публичного okx.RestConfig
// в корневом пакете (там делается явная конвертация структур, чтобы избежать
// import-cycle).
type Config struct {
	RequestTimeout      time.Duration
	MaxIdleConns        int
	MaxIdleConnsPerHost int
	IdleConnTimeout     time.Duration
	// Demo — если true, на каждый запрос добавляется заголовок
	// "x-simulated-trading: 1" — OKX переключает обработку в режим paper-trading.
	Demo bool
	// RateLimitObserver — опциональный legacy-callback, вызывается СИНХРОННО
	// после получения HTTP-ответа (и до парсинга тела) с собранными rate-limit
	// заголовками. nil → no-op. Подробный контракт — см. okx.Config в корневом
	// пакете (там это поле публикуется конечному пользователю SDK).
	RateLimitObserver func(endpoint string, headers map[string]string)
	// RateLimitEventObserver — расширенный callback (v2.2.0+). Принимает
	// метаданные запроса (endpoint, method, headers, RequestMeta), которые
	// корневой okx.Client конвертирует в публичный okx.RateLimitEvent.
	// nil → no-op. Если заданы оба observer'а — оба вызываются последовательно.
	RateLimitEventObserver func(endpoint, method string, headers map[string]string, meta RequestMeta)
}

// RequestMeta — метаданные запроса, известные на стороне domain-слоя
// (swap/trading.go, swap/account.go), которые нужны внешнему rate-limiter'у
// для точного учёта OKX лимитов. Заполняется вызывающим методом и
// прокидывается через rest.Options в RateLimitEventObserver. Если пусто —
// observer получит нулевые значения (count=0, нет symbols, category="").
type RequestMeta struct {
	// OrderCount — сколько ордеров затрагивает запрос. 1 для single, N
	// для batch, 0 для не-trading. См. okx.RateLimitEvent.OrderCount.
	OrderCount int
	// Symbols — список OKX InstID. См. okx.RateLimitEvent.Symbols.
	Symbols []string
	// Category — строковое представление okx.RateLimitCategory
	// ("place"/"amend"/"cancel"/"query"/"market"/""). Передаём как
	// строку, чтобы не возникал import-cycle internal/rest ↔ корневой okx.
	Category string
}

// Options — параметры одного REST-запроса.
type Options struct {
	Method string
	Path   string
	Query  url.Values
	Body   any
	Signed bool
	// Meta — метаданные для RateLimitEventObserver. Если zero — observer
	// получает нули. Заполняется доменными методами swap/* там, где
	// известны instId / batch size / категория запроса.
	Meta RequestMeta
}

// Response — обобщённая обёртка ответа OKX:
//
//	{ "code":"0", "msg":"", "data":[...] }
type Response struct {
	Code string             `json:"code"`
	Msg  string             `json:"msg"`
	Data jsoniterRawMessage `json:"data"`
}

// jsoniterRawMessage — аналог json.RawMessage, корректно работающий с jsoniter.
type jsoniterRawMessage []byte

// MarshalJSON реализует json.Marshaler.
func (m jsoniterRawMessage) MarshalJSON() ([]byte, error) {
	if len(m) == 0 {
		return []byte("null"), nil
	}
	return []byte(m), nil
}

// UnmarshalJSON реализует json.Unmarshaler.
func (m *jsoniterRawMessage) UnmarshalJSON(data []byte) error {
	*m = append((*m)[:0], data...)
	return nil
}

// UnmarshalData разворачивает поле data ответа в произвольный dest.
func (r Response) UnmarshalData(dest any) error {
	if len(r.Data) == 0 || bytes.Equal(r.Data, []byte("null")) {
		return nil
	}
	return codec.Unmarshal(r.Data, dest)
}

// Client — низкоуровневый REST-клиент.
type Client struct {
	httpClient             *http.Client
	signer                 *auth.Signer
	baseURL                string
	userAgent              string
	logger                 okxlog.Logger
	demo                   bool
	rateLimitObserver      func(endpoint string, headers map[string]string)
	rateLimitEventObserver func(endpoint, method string, headers map[string]string, meta RequestMeta)
}

// NewClient создаёт REST-клиент.
func NewClient(baseURL string, signer *auth.Signer, cfg Config, ua string, log okxlog.Logger) *Client {
	if log == nil {
		log = okxlog.Noop()
	}
	var transport *http.Transport = &http.Transport{
		MaxIdleConns:        cfg.MaxIdleConns,
		MaxIdleConnsPerHost: cfg.MaxIdleConnsPerHost,
		IdleConnTimeout:     cfg.IdleConnTimeout,
		ForceAttemptHTTP2:   true,
	}
	var httpClient *http.Client = &http.Client{
		Timeout:   cfg.RequestTimeout,
		Transport: transport,
	}
	return &Client{
		httpClient:             httpClient,
		signer:                 signer,
		baseURL:                strings.TrimRight(baseURL, "/"),
		userAgent:              ua,
		logger:                 log,
		demo:                   cfg.Demo,
		rateLimitObserver:      cfg.RateLimitObserver,
		rateLimitEventObserver: cfg.RateLimitEventObserver,
	}
}

// Close закрывает idle-соединения транспорта.
func (c *Client) Close() {
	if c == nil || c.httpClient == nil {
		return
	}
	if t, ok := c.httpClient.Transport.(*http.Transport); ok {
		t.CloseIdleConnections()
	}
}

/*
Do выполняет один REST-вызов и возвращает обёртку Response + rate-limit
заголовки + ошибку. Семантика ошибок — см. документацию пакета.
*/
func (c *Client) Do(ctx context.Context, opts Options) (Response, map[string]string, error) {
	var resp Response
	var rateLimits map[string]string

	var fullURL string
	var bodyStr string
	var err error
	fullURL, bodyStr, err = c.buildRequest(opts)
	if err != nil {
		return resp, rateLimits, err
	}

	var req *http.Request
	req, err = http.NewRequestWithContext(ctx, strings.ToUpper(opts.Method), fullURL, bytes.NewBufferString(bodyStr))
	if err != nil {
		return resp, rateLimits, okxerr.New(okxerr.ErrorKindInvalidRequest, "", "rest: build request", err)
	}

	c.applyHeaders(req, opts, bodyStr)

	var httpResp *http.Response
	var started time.Time = time.Now()
	httpResp, err = c.httpClient.Do(req)
	if err != nil {
		return resp, rateLimits, classifyTransportError(err)
	}
	defer func() {
		_ = httpResp.Body.Close()
	}()

	rateLimits = collectRateLimitHeaders(httpResp.Header)
	// Уведомляем observer'ов ДО парсинга тела: даже если ответ невалидный JSON
	// или содержит OKX-ошибку, rate-limit информация всё равно полезна для
	// внешнего rate-limiter'а (например, чтобы он не блокировал retry).
	// Гарантия non-nil map в observer — упрощает подписчика (см. okx.Config).
	//
	// Если заданы оба observer'а — оба вызываются последовательно. Порядок:
	// сначала legacy (RateLimitObserver), потом event (RateLimitEventObserver).
	// Это позволяет постепенно мигрировать существующих подписчиков, не теряя
	// событий ни на одной стороне.
	if c.rateLimitObserver != nil || c.rateLimitEventObserver != nil {
		var hdrs map[string]string = rateLimits
		if hdrs == nil {
			hdrs = map[string]string{}
		}
		if c.rateLimitObserver != nil {
			c.rateLimitObserver(opts.Path, hdrs)
		}
		if c.rateLimitEventObserver != nil {
			c.rateLimitEventObserver(opts.Path, strings.ToUpper(opts.Method), hdrs, opts.Meta)
		}
	}

	var raw []byte
	raw, err = io.ReadAll(httpResp.Body)
	if err != nil {
		return resp, rateLimits, okxerr.New(okxerr.ErrorKindNetwork, "", "rest: read body", err)
	}

	c.logger.Debug(
		"rest.Do",
		okxlog.Str("method", opts.Method),
		okxlog.Str("path", opts.Path),
		okxlog.Int("status", int64(httpResp.StatusCode)),
		okxlog.Int("durationMs", time.Since(started).Milliseconds()),
		okxlog.Int("bytes", int64(len(raw))),
	)

	if httpResp.StatusCode >= 200 && httpResp.StatusCode < 300 {
		if err = codec.Unmarshal(raw, &resp); err != nil {
			return resp, rateLimits, okxerr.New(okxerr.ErrorKindUnknown, "", "rest: parse response", err)
		}
		// OKX-семантика top-level code:
		//   "0"      — success;
		//   "1"      — bulk error: ВСЕ элементы data[] упали (см. sCode/sMsg);
		//   "2"      — bulk partial: часть элементов data[] упала;
		//   прочее   — fatal на уровне запроса (auth, rate-limit, validation).
		// Для "1" и "2" мы отдаём data наверх, чтобы domain-слой (Trading и т.п.)
		// извлёк per-entry sCode/sMsg и собрал точную ошибку. Без этого
		// пользователь видит бесполезное "All operations failed".
		if resp.Code != "" && resp.Code != "0" && resp.Code != "1" && resp.Code != "2" {
			return resp, rateLimits, &okxerr.Error{
				Kind:       okxerr.MapOKXCode(resp.Code, resp.Msg),
				HTTPStatus: httpResp.StatusCode,
				OKXCode:    resp.Code,
				Message:    resp.Msg,
			}
		}
		return resp, rateLimits, nil
	}

	if err = codec.Unmarshal(raw, &resp); err == nil && resp.Code != "" {
		return resp, rateLimits, &okxerr.Error{
			Kind:       okxerr.MapOKXCode(resp.Code, resp.Msg),
			HTTPStatus: httpResp.StatusCode,
			OKXCode:    resp.Code,
			Message:    resp.Msg,
		}
	}
	return resp, rateLimits, &okxerr.Error{
		Kind:       okxerr.MapHTTPStatus(httpResp.StatusCode),
		HTTPStatus: httpResp.StatusCode,
		Message:    truncate(string(raw), 256),
	}
}

// buildRequest собирает URL и сериализованное тело.
func (c *Client) buildRequest(opts Options) (string, string, error) {
	var u *url.URL
	var err error
	u, err = url.Parse(c.baseURL + opts.Path)
	if err != nil {
		return "", "", okxerr.New(okxerr.ErrorKindInvalidRequest, "", "rest: invalid url", err)
	}
	if len(opts.Query) > 0 {
		u.RawQuery = opts.Query.Encode()
	}

	var body string
	if opts.Body != nil {
		var raw []byte
		raw, err = codec.Marshal(opts.Body)
		if err != nil {
			return "", "", okxerr.New(okxerr.ErrorKindInvalidRequest, "", "rest: marshal body", err)
		}
		body = string(raw)
	}
	return u.String(), body, nil
}

// applyHeaders проставляет стандартные и подписанные заголовки.
func (c *Client) applyHeaders(req *http.Request, opts Options, body string) {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", c.userAgent)
	req.Header.Set("Accept", "application/json")
	if c.demo {
		req.Header.Set("x-simulated-trading", "1")
	}

	if !opts.Signed {
		return
	}
	if c.signer == nil || !c.signer.Enabled() {
		return
	}

	var ts string = c.signer.IsoTimestamp(time.Now())
	var signPath string = opts.Path
	if len(opts.Query) > 0 {
		signPath = signPath + "?" + opts.Query.Encode()
	}

	var signature string
	var err error
	signature, err = c.signer.Sign(ts, strings.ToUpper(opts.Method), signPath, body)
	if err != nil {
		c.logger.Warn("rest: sign skipped", okxlog.Err(err))
		return
	}
	req.Header.Set("OK-ACCESS-KEY", c.signer.APIKey())
	req.Header.Set("OK-ACCESS-SIGN", signature)
	req.Header.Set("OK-ACCESS-TIMESTAMP", ts)
	req.Header.Set("OK-ACCESS-PASSPHRASE", c.signer.Passphrase())
}

// classifyTransportError превращает сетевую/ctx-ошибку в *okxerr.Error.
func classifyTransportError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) {
		return okxerr.New(okxerr.ErrorKindNetwork, "", "rest: context canceled", err)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return okxerr.New(okxerr.ErrorKindNetwork, "", "rest: deadline exceeded", err)
	}
	return okxerr.New(okxerr.ErrorKindNetwork, "", "rest: transport error", err)
}

// collectRateLimitHeaders собирает фиксированный набор заголовков.
func collectRateLimitHeaders(h http.Header) map[string]string {
	var out map[string]string
	var v string
	for _, key := range rateLimitHeaders {
		v = h.Get(key)
		if v == "" {
			continue
		}
		if out == nil {
			out = make(map[string]string, 4)
		}
		out[key] = v
	}
	return out
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
