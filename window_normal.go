//go:build !transparent

package main

import "github.com/wailsapp/wails/v3/pkg/application"

var isFrameless = false

func configureWindow(app *application.App) {
	app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:            "MotorRoller",
		Width:            1000,
		Height:           700,
		BackgroundColour: application.NewRGB(36, 36, 36), // xianii --color-base-100
	})
}
