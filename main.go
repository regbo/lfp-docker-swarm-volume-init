package main

func main() {
	if app, err := NewAppBuilder().Build(); err != nil {
		panic(err)
	} else if err = app.Run(); err != nil {
		panic(err)
	}
}
