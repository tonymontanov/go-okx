/*
ФАЙЛ: swap/doc.go

ОПИСАНИЕ:
Пакет swap реализует SWAP-профиль OKX (USD-M Perpetual). Это «толстый»
доменный клиент по архитектуре Variant B (см. ТЗ §7): пользователь получает
четыре саб-клиента — Trading, Account, MarketData, Stream — каждый со своими
методами в идиоматичном Go-стиле.

ПУБЛИЧНЫЙ ВХОД:
  - swap.NewClient(parent *okx.Client) *Client    — обычный путь;
  - parent.Swap().(*swap.Client)                  — то же самое лениво, без
                                                    дублирования передачи parent.

САБ-КЛИЕНТЫ:
  - (*Client).Trading()    : CreateOrder, ModifyOrder, CancelOrder, batch-варианты, CancelAll, CancelForgotten.
  - (*Client).Account()    : GetPosition/GetOpenOrders/ClosePosition/SetLeverage/SetPositionMode.
  - (*Client).MarketData() : GetSymbolInfo/GetOrderBook/GetHistoricalCandles.
  - (*Client).Stream()     : Watch* (WebSocket-подписки). Реализуется в M3.

ТИПЫ:
Все доменные структуры (CreateOrderRequest, OrderInfo, PositionInfo, …) живут
в подпакете swap/types и используются и саб-клиентами SDK, и адаптером деска.

В рамках v1 поддерживается ТОЛЬКО SWAP (instType=SWAP). SPOT-профиль будет
реализован отдельно (пакет spot).
*/
package swap
