//go:build !ui

package ui

var (
	ch = make(chan struct{})
)

func Stop() {
	close(ch)
}
func Run() {
	<-ch
}
