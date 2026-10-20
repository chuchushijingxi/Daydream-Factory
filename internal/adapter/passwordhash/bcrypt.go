package passwordhash

import "golang.org/x/crypto/bcrypt"

type Bcrypt struct{}

func (Bcrypt) Hash(raw []byte) ([]byte, error) {
	return bcrypt.GenerateFromPassword(raw, bcrypt.DefaultCost)
}
