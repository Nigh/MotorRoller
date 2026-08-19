package main

import (
	"embed"

	"github.com/wailsapp/wails/v3/pkg/application"
)

//go:embed frontend/dist
var assets embed.FS

func main() {
	svc := &AppService{}
	app := application.New(application.Options{
		Name: "MotorRoller",
		Services: []application.Service{
			application.NewService(svc),
		},
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(assets),
		},
	})
	svc.setApp(app)

	configureWindow(app)

	if err := app.Run(); err != nil {
		println("Error:", err.Error())
	}
}
