/*
ФАЙЛ: spot/rest-doer.go

ОПИСАНИЕ:
Минимальный интерфейс REST-транспорта для саб-клиентов SPOT. Зеркало
swap/rest-doer.go — тот же контракт, что у internal/rest.Client.Do.
Используется для тестируемости (подмена fake-rest в unit-тестах).
*/

package spot

import (
	"context"

	"github.com/tonymontanov/go-okx/v2/internal/rest"
)

// restDoer — минимальный контракт REST-транспорта.
type restDoer interface {
	Do(ctx context.Context, opts rest.Options) (rest.Response, map[string]string, error)
}
