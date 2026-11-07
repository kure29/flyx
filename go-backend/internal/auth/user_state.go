package auth

type UserAuthState struct {
	ID                int64
	RoleID            int
	Status            int
	PasswordChangedAt int64
}
