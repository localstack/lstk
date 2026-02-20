package ports

import (
	"fmt"
	"net"
	"time"
)

func CheckAvailable(port string) error {
	conn, err := net.DialTimeout("tcp", "localhost:"+port, time.Second)
	if err != nil {
		return nil
	}
	_ = conn.Close()
	return fmt.Errorf("port %s already in use", port)
}
