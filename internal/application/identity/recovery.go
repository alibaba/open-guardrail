package identity

import "context"

// ResetLocalAdmin is a host-maintenance capability, never exposed through HTTP.
// An empty username preserves the existing name. All writes and session revocation
// are atomic; policy data, user identity and machine keys remain unchanged.
func (s *Service) ResetLocalAdmin(ctx context.Context, username, password string) error {
	if (username != "" && !usernamePattern.MatchString(username)) || !ValidPassword(password) {
		return ErrInvalid
	}
	return s.Repo.Transact(ctx, func(tx Transaction) error {
		state, err := tx.State()
		if err != nil {
			return err
		}
		if !state.Initialized {
			return ErrUnauthorized
		}
		user, err := tx.User()
		if err != nil {
			return err
		}
		if username != "" {
			user.Username = username
		}
		if err = s.setPassword(tx, user, password); err != nil {
			return err
		}
		return tx.Audit("admin_reset_local", "local-host")
	})
}
