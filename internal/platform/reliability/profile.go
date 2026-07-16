package reliability

import (
	platformmodule "github.com/campusos/CampusOS/internal/platform/module"
	"github.com/campusos/CampusOS/internal/platform/transaction"
	"github.com/jackc/pgx/v5/pgxpool"
)

func BindPostgreSQLAdapter(app *platformmodule.AppContext, pool *pgxpool.Pool) error {
	if err := app.Provide(portStore, Store(NewPostgreSQLStore(pool))); err != nil {
		return err
	}
	return app.Provide(portTransactions, transaction.Manager(transaction.NewPostgreSQL(pool)))
}

func BindMemoryAdapter(app *platformmodule.AppContext) error {
	if err := app.Provide(portStore, Store(NewMemoryStore())); err != nil {
		return err
	}
	return app.Provide(portTransactions, transaction.Manager(transaction.NewMemory()))
}
