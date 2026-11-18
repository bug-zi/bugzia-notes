module bugzia-notes

go 1.27.1

require (
	github.com/google/uuid v1.6.0
	github.com/wailsapp/wails/v3 v3.1.0
	golang.org/x/sys v0.50.0
)

// 本地补丁副本：Windows 透明窗口需经 ICoreWebView2ControllerOptions3 在
// controller 创建时注入 alpha=0（上游只在创建后 PutDefaultBackgroundColor，
// 半透明内容被垫白）。详见 .wails-patched 内 ICoreWebView2Environment10.go。
replace github.com/wailsapp/wails/v3 => ./.wails-patched/wails-v3.1.0

require (
	github.com/coder/websocket v1.8.14 // indirect
	github.com/ebitengine/purego v0.11.1 // indirect
	github.com/go-ole/go-ole v1.3.0 // indirect
	github.com/godbus/dbus/v5 v5.2.2 // indirect
	golang.org/x/term v0.46.0 // indirect
)
