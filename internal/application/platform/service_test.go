package platform

import (
	"context"
	"errors"
	"testing"

	"drama-agent/internal/application/ports"
	"drama-agent/internal/domain/fault"
)

func TestWritesRequireOperatorAndRespectReadOnlyOutsideHTTP(t *testing.T) {
	for _, row := range []struct {
		actor    ports.Actor
		readOnly bool
		code     string
	}{
		{ports.Actor{}, false, "AUTH_REQUIRED"},
		{ports.Actor{UserID: "user", Role: ports.RoleUser}, false, "ROLE_FORBIDDEN"},
		{ports.Actor{UserID: "op", Role: ports.RoleOperator}, true, "SERVICE_READ_ONLY"},
		{ports.Actor{UserID: "op", Role: ports.RoleOperator}, false, "PLATFORM_SETTINGS_UNAVAILABLE"},
	} {
		s := &Service{ReadOnly: row.readOnly}
		_, save := s.Save(context.Background(), row.actor, Input{})
		_, upload := s.UploadLogo(context.Background(), row.actor, nil)
		for _, err := range []error{save, upload} {
			var problem *fault.Error
			if !errors.As(err, &problem) || problem.Code != row.code {
				t.Fatal("write reached storage without required guard", err, row.code)
			}
		}
	}
}
