package main

func main() {
	app := buildApp()
	if err := app.Run(); err != nil {
		panic(err)
	}
}
