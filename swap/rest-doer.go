/*
ФАЙЛ: swap/rest-doer.go

ОПИСАНИЕ:
Файл объявляет минимальный интерфейс restDoer, который умеют делать REST-вызовы
и парсить общую обёртку OKX. Использование интерфейса (а не *rest.Client напрямую)
даёт две выгоды:
  1. Тестируемость: в unit-тестах саб-клиентов мы передаём fake-rest без
     реального http.Client'а.
  2. Изоляция: код swap-пакета не зависит от деталей конкретной реализации
     транспорта — теоретически REST-клиент можно подменить на async pipeline.

restDoer полностью совпадает с публичным API *internal/rest.Client.Do.
*/

package swap

import (
	"context"

	"github.com/tonymontanov/go-okx/v2/internal/rest"
)

// restDoer — минимальный контракт REST-транспорта.
type restDoer interface {
	Do(ctx context.Context, opts rest.Options) (rest.Response, map[string]string, error)
}
