/*
ФАЙЛ: swap/types/balance.go

ОПИСАНИЕ:
Модель ответа GET /api/v5/account/balance для SWAP-профиля. С момента
выделения общего слоя — type-alias на commontypes.Balance/BalanceDetail
(OKX использует unified-account; модель идентична для spot и swap).

Документация — в types/balance.go.
*/

package types

import commontypes "github.com/tonymontanov/go-okx/v2/types"

// Balance — состояние unified-account. См. commontypes.Balance.
type Balance = commontypes.Balance

// BalanceDetail — баланс одной валюты. См. commontypes.BalanceDetail.
type BalanceDetail = commontypes.BalanceDetail
