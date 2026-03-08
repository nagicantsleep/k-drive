package storage

import "context"

type Account struct {
	ID       string
	Provider string
	Email    string
	Options  map[string]string
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
	copiedOptions := make(map[string]string)
	for key, value := range account.Options {
		copiedOptions[key] = value
	}
	account.Options = copiedOptions
	r.accounts = append(r.accounts, account)
	return nil
}

func (r *MemoryAccountRepository) List(_ context.Context) ([]Account, error) {
	result := make([]Account, 0, len(r.accounts))
	for _, account := range r.accounts {
		copiedOptions := make(map[string]string)
		for key, value := range account.Options {
			copiedOptions[key] = value
		}
		account.Options = copiedOptions
		result = append(result, account)
	}
	return result, nil
}
