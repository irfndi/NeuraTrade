package trading_test

import (
	"fmt"

	"github.com/irfndi/goflux/pkg/decimal"
	"github.com/irfndi/goflux/pkg/trading"
)

// Example_basicPosition demonstrates basic position creation and management
func Example_basicPosition() {
	order := trading.Order{
		Side:   trading.BUY,
		Amount: decimal.New(10),
		Price:  decimal.New(100),
	}
	position := trading.NewPosition(order)
	fmt.Printf("Position opened: %t\n", position.IsOpen())
	fmt.Printf("Cost basis: %s\n", position.CostBasis())
}
