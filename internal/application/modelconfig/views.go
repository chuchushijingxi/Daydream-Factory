package modelconfig

import (
	"context"

	"drama-agent/internal/application/ports"
	md "drama-agent/internal/domain/modelconfig"
)

type ModelConfigView struct {
	Revision    uint64           `json:"revision"`
	Editable    bool             `json:"editable"`
	Connections []ConnectionView `json:"connections"`
}

type ConnectionView struct {
	ID                   string                `json:"id"`
	Name                 string                `json:"name"`
	Kind                 string                `json:"kind"`
	Protocol             string                `json:"protocol"`
	BaseURL              string                `json:"baseUrl"`
	CredentialConfigured bool                  `json:"credentialConfigured"`
	Editable             bool                  `json:"editable"`
	Reason               string                `json:"reason"`
	Models               []ConfiguredModelView `json:"models"`
}

type ConfiguredModelView struct {
	MediaProfile *md.MediaProfile `json:"mediaProfile"`
	md.ConfiguredModel
	LogoURL        string `json:"logoUrl"`
	DefaultLogoURL string `json:"defaultLogoUrl"`
	Name           string `json:"name"`
	DefaultName    string `json:"defaultName"`
	Provider       string `json:"provider"`
	Available      bool   `json:"available"`
	Reason         string `json:"reason"`
	Referenced     bool   `json:"referenced"`
}

func Provider(m md.ConfiguredModel) string {
	if md.IsMockModel(m.ModelID, m.Category) {
		return "mock"
	}
	if md.IsDeepSeekModel(m.ModelID) {
		return "deepseek"
	}
	if md.IsVolcengineModel(m.ModelID) {
		return "volcengine"
	}
	if md.IsKlingModel(m.ModelID, m.Category) {
		return "kling"
	}
	if md.IsAlibabaModel(m.ModelID) {
		return "alibaba"
	}
	if md.IsGoogleModel(m.ModelID) {
		return "google"
	}
	if md.IsOpenAIModel(m.ModelID) {
		return "openai"
	}
	return "custom"
}

func (c *Service) view(cfg md.ModelConfig, projects []ports.ProjectModelReference) (ModelConfigView, error) {
	out := ModelConfigView{Revision: cfg.Revision, Editable: !c.ReadOnly && c.Cipher != nil && c.Factory != nil, Connections: []ConnectionView{}}
	for _, conn := range cfg.Connections {
		var err error
		conn, err = md.ResolveOfficial(conn)
		if err != nil {
			return out, err
		}
		v := ConnectionView{ID: conn.ID, Name: conn.Name, Kind: conn.Kind, Protocol: conn.Protocol, BaseURL: conn.BaseURL, CredentialConfigured: len(conn.Secret) > 0, Editable: !c.ReadOnly && c.Cipher != nil && c.Factory != nil && !connectionActive(conn, projects), Models: []ConfiguredModelView{}}
		local := conn.Kind == "builtin" && conn.ID == "mock" && conn.Protocol == md.MockProtocol
		switch {
		case c.ReadOnly:
			v.Reason = "当前服务为只读模式，不能配置或调用模型"
		case c.Cipher == nil || c.Factory == nil:
			v.Reason = "模型配置资源不可用"
		case connectionActive(conn, projects):
			v.Reason = "任务正在使用该连接，结束后可修改配置"
		case len(conn.Secret) == 0 && !local:
			v.Reason = "请先配置 API Key"
		}
		for _, m := range conn.Models {
			mv := ConfiguredModelView{ConfiguredModel: m, LogoURL: LogoURL(m), DefaultLogoURL: DefaultLogoURL(m), Name: m.DisplayName(), DefaultName: m.DefaultName(), Provider: Provider(m), Reason: v.Reason, Referenced: modelReferenced(m.ID, projects)}
			if conn.Kind != "gateway" {
				mv.MediaProfile = md.OfficialMediaProfile(conn.ID, m.ModelID)
				mv.ResolutionEnabled = m.ResolutionSwitches(mv.MediaProfile)
			}
			mv.Available = m.Enabled && len(conn.Secret) > 0 && !c.ReadOnly && c.Cipher != nil && c.Factory != nil && m.Executable()
			switch {
			case mv.MediaProfile != nil && mv.MediaProfile.Retired:
				mv.Available = false
				mv.Reason = mv.MediaProfile.Note
			case mv.MediaProfile != nil && len(m.EnabledMediaProfile(mv.MediaProfile).Resolutions) == 0:
				mv.Available = false
				mv.Reason = "未开启可用档位，不会出现在新任务候选中"
			case local && md.IsMockModel(m.ModelID, m.Category):
				mv.Available = m.Enabled && !c.ReadOnly && c.Tasks != nil
				if c.ReadOnly {
					mv.Reason = v.Reason
				} else if c.Tasks == nil {
					mv.Reason = "本地测试适配器不可用"
				} else if !m.Enabled {
					mv.Reason = "模型已关闭，不会出现在工作台候选中"
				} else {
					mv.Reason = "本地模拟媒体，按已保存的延迟及失败概率执行"
				}
			case m.Category != "analysis":
				mv.Reason = "模型已登记，该类别的实际生成尚未接入"
			case !m.Executable():
				mv.Reason = "请补充模型容量与文本计价后使用"
			case mv.Reason == "" && !m.Enabled:
				mv.Reason = "模型已关闭，不会出现在项目候选中"
			}
			v.Models = append(v.Models, mv)
		}
		out.Connections = append(out.Connections, v)
	}
	return out, nil
}

func (c *Service) configView(ctx context.Context) (ModelConfigView, error) {
	cfg, e := c.Snapshot(ctx)
	if e != nil {
		return ModelConfigView{}, e
	}
	ps, e := c.Projects.ModelReferences(ctx)
	if e != nil {
		return ModelConfigView{}, e
	}
	return c.view(cfg, ps)
}
