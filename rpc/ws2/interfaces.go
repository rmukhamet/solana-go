package ws2

type Logger interface {
	Info(...interface{})
	Error(...interface{})
	Infof(string, ...interface{})
	Errorf(string, ...interface{})
}
