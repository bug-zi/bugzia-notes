//go:build windows

// 透明背景补丁新增绑定：Wails 原生只在 controller 创建后调用
// PutDefaultBackgroundColor，窗口 hosting 模式下半透明页面内容仍被垫白
// （漂白）。WebView2 只有在 controller 创建时经
// ICoreWebView2ControllerOptions3::SetDefaultBackgroundColor 注入 alpha=0，
// 才会以 alpha-aware 合成路径构建（Tauri/wry 同款做法）。
// GUID 与 vtbl 槽位布局取自 webview2-com-sys 0.38.2（wry 的绑定源）。

package edge

import (
	"log"
	"unsafe"

	"golang.org/x/sys/windows"
)

// ICoreWebView2Environment10：Environment1..9 自有方法共 17 个
// （5+1+2+1+2+1+1+3+1，槽位数已对照 webview2-com-sys 0.38.2 bindings.rs
// 逐接口核实），随后是本接口的 3 个方法。
type iCoreWebView2Environment10Vtbl struct {
	_IUnknownVtbl
	_                                                 [17]ComProc
	CreateCoreWebView2ControllerOptions               ComProc
	CreateCoreWebView2ControllerWithOptions           ComProc
	CreateCoreWebView2CompositionControllerWithOptions ComProc
}

type ICoreWebView2Environment10 struct {
	vtbl *iCoreWebView2Environment10Vtbl
}

func (e *ICoreWebView2Environment) GetICoreWebView2Environment10() *ICoreWebView2Environment10 {
	var result *ICoreWebView2Environment10
	iid := NewGUID("{EE0EB9DF-6F12-46CE-B53F-3F47B9C928E0}")
	_, _, _ = e.vtbl.QueryInterface.Call(
		uintptr(unsafe.Pointer(e)),
		uintptr(unsafe.Pointer(iid)),
		uintptr(unsafe.Pointer(&result)))
	return result
}

func (e *ICoreWebView2Environment10) Release() {
	_, _, _ = e.vtbl.Release.Call(uintptr(unsafe.Pointer(e)))
}

// ICoreWebView2ControllerOptions：基接口，自有方法 ProfileName、
// IsInPrivateModeEnabled 的 get/put 共 4 个（QueryInterface 转型用）。
type iCoreWebView2ControllerOptionsVtbl struct {
	_IUnknownVtbl
	_ [4]ComProc
}

type iCoreWebView2ControllerOptions struct {
	vtbl *iCoreWebView2ControllerOptionsVtbl
}

func (o *iCoreWebView2ControllerOptions) Release() {
	_, _, _ = o.vtbl.Release.Call(uintptr(unsafe.Pointer(o)))
}

// ICoreWebView2ControllerOptions3：基链自有方法 Options 4 个 + Options2
// 2 个 + DefaultBackgroundColor get，占位 7 槽后是 put。
type iCoreWebView2ControllerOptions3Vtbl struct {
	_IUnknownVtbl
	_                         [7]ComProc
	SetDefaultBackgroundColor ComProc
}

type ICoreWebView2ControllerOptions3 struct {
	vtbl *iCoreWebView2ControllerOptions3Vtbl
}

func (o *iCoreWebView2ControllerOptions) GetICoreWebView2ControllerOptions3() *ICoreWebView2ControllerOptions3 {
	var result *ICoreWebView2ControllerOptions3
	iid := NewGUID("{B32B191A-8998-57CA-B7CB-E04617E4CE4A}")
	_, _, _ = o.vtbl.QueryInterface.Call(
		uintptr(unsafe.Pointer(o)),
		uintptr(unsafe.Pointer(iid)),
		uintptr(unsafe.Pointer(&result)))
	return result
}

func (o *ICoreWebView2ControllerOptions3) Release() {
	_, _, _ = o.vtbl.Release.Call(uintptr(unsafe.Pointer(o)))
}

func (o *ICoreWebView2ControllerOptions3) SetDefaultBackgroundColor(color COREWEBVIEW2_COLOR) error {
	// Cast to a uint32 as that's what the call is expecting
	col := *(*uint32)(unsafe.Pointer(&color))

	hr, _, _ := o.vtbl.SetDefaultBackgroundColor.Call(
		uintptr(unsafe.Pointer(o)),
		uintptr(col),
	)
	if windows.Handle(hr) != windows.S_OK {
		return windows.Errno(hr)
	}
	return nil
}

func (e *ICoreWebView2Environment10) CreateCoreWebView2ControllerOptions() (*iCoreWebView2ControllerOptions, error) {
	var opts *iCoreWebView2ControllerOptions
	hr, _, _ := e.vtbl.CreateCoreWebView2ControllerOptions.Call(
		uintptr(unsafe.Pointer(e)),
		uintptr(unsafe.Pointer(&opts)),
	)
	if windows.Handle(hr) != windows.S_OK {
		return nil, windows.Errno(hr)
	}
	return opts, nil
}

func (e *ICoreWebView2Environment10) CreateCoreWebView2ControllerWithOptions(parentWindow uintptr, options *iCoreWebView2ControllerOptions, handler *iCoreWebView2CreateCoreWebView2ControllerCompletedHandler) error {
	hr, _, _ := e.vtbl.CreateCoreWebView2ControllerWithOptions.Call(
		uintptr(unsafe.Pointer(e)),
		parentWindow,
		uintptr(unsafe.Pointer(options)),
		uintptr(unsafe.Pointer(handler)),
	)
	if windows.Handle(hr) != windows.S_OK {
		return windows.Errno(hr)
	}
	return nil
}

// CreateCoreWebView2ControllerTransparent 经 controller options 在创建时
// 注入透明默认底色。创建时注入才会启用 alpha-aware 合成；创建后运行时
// PutDefaultBackgroundColor 改不回来。旧 runtime 缺接口时返回
// UnsupportedCapabilityError，调用方回退老路径。
func (e *ICoreWebView2Environment) CreateCoreWebView2ControllerTransparent(hwnd uintptr, handler *iCoreWebView2CreateCoreWebView2ControllerCompletedHandler) error {
	env10 := e.GetICoreWebView2Environment10()
	if env10 == nil {
		log.Printf("[WebView2] QI ICoreWebView2Environment10 failed")
		return UnsupportedCapabilityError
	}
	defer env10.Release()

	opts, err := env10.CreateCoreWebView2ControllerOptions()
	if err != nil {
		log.Printf("[WebView2] CreateCoreWebView2ControllerOptions failed: %v", err)
		return err
	}
	defer opts.Release()

	opts3 := opts.GetICoreWebView2ControllerOptions3()
	if opts3 == nil {
		log.Printf("[WebView2] QI ICoreWebView2ControllerOptions3 failed")
		return UnsupportedCapabilityError
	}
	defer opts3.Release()

	if err := opts3.SetDefaultBackgroundColor(COREWEBVIEW2_COLOR{A: 0, R: 0, G: 0, B: 0}); err != nil {
		log.Printf("[WebView2] SetDefaultBackgroundColor(transparent) via options failed: %v", err)
		return err
	}

	err = env10.CreateCoreWebView2ControllerWithOptions(hwnd, opts, handler)
	return err
}
