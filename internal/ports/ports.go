package ports

import (
	"fmt"
	"net"
	"time"

	"github.com/localstack/lstk/internal/validate"
)

func CheckAvailable(port string) error {
	if err := validate.Port(port); err != nil {
		return err
	}
	conn, err := net.DialTimeout("tcp", "localhost:"+port, time.Second)
	if err != nil {
		return nil
	}
	_ = conn.Close()
	return fmt.Errorf("port %s already in use", port)
}
