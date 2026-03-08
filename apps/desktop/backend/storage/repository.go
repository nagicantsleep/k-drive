package storage

import "context"

type Account struct {
	ID       string
	Provider string
	Email    string
}

type AccountRepository interface {
	Save(ctx context.Context, account Account) error
	List(ctx context.Context) ([]Account, error)
}

type MemoryAccountRepository struct {
	accounts []Account
}

func NewAccountRepository() *MemoryAccountRepository {
	return &MemoryAccountRepository{accounts: []Account{}}
}

func (r *MemoryAccountRepository) Save(_ context.Context, account Account) error {
	r.accounts = append(r.accounts, account)
	return nil
}

func (r *MemoryAccountRepository) List(_ context.Context) ([]Account, error) {
	result := make([]Account, len(r.accounts))
	copy(result, r.accounts)
	return result, nil
}
