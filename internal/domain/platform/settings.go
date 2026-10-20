package platform

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"drama-agent/internal/domain/fault"
	"drama-agent/internal/domain/logoimage"
)

type Settings struct {
	Name               string `json:"name"`
	SidebarDescription string `json:"sidebarDescription"`
	LogoID             string `json:"logoId"`
}

func Validate(v Settings) (Settings, error) {
	v.Name = strings.TrimSpace(v.Name)
	v.SidebarDescription = strings.TrimSpace(v.SidebarDescription)
	nameLength := utf8.RuneCountInString(v.Name)
	invalidName := !utf8.ValidString(v.Name) || nameLength < 1 || nameLength > 48
	invalidDescription := !utf8.ValidString(v.SidebarDescription) || utf8.RuneCountInString(v.SidebarDescription) > 120
	hasControl := strings.ContainsFunc(v.Name+v.SidebarDescription, unicode.IsControl)
	if invalidName || invalidDescription || hasControl {
		return Settings{}, fault.New("PLATFORM_SETTINGS_INVALID", "平台名称须为1～48字，侧栏描述最多120字，不能包含控制字符")
	}
	if v.LogoID != "" && !logoimage.ValidID(v.LogoID) {
		return Settings{}, fault.New("INVALID_PLATFORM_LOGO", "请上传有效平台Logo或恢复默认Logo")
	}
	return v, nil
}
