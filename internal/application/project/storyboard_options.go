package project

import (
	"context"
	"encoding/json"
	"fmt"
	"math"

	"drama-agent/internal/domain/fault"
	d "drama-agent/internal/domain/project"
)

func breakdownInstruction(mode string) (string, error) {
	switch mode {
	case "", "general":
		return "通用型：优先保证剧情清楚、因果连续、节奏自然。先明确各场景的核心事件，再组织连续镜头。每镜承担清楚的表达任务，避免逐句切镜和重复表达；兼顾叙事与镜头连贯。", nil
	case "story":
		return "剧情友好：优先表达原文已有的冲突、情绪推进、信息揭示和人物反应。关键动作、态度变化及反转发生时，选择最能让观众理解和感受到变化的画面。可以安排必要的反应镜头，不机械增加碎切；不得为了钩子、悬念或反转新增剧情、重排因果或删改对白。", nil
	case "production":
		return "镜头友好：优先保证空间关系清楚、动作连续、人物与道具状态一致。同地点、同时间、同一表达目的的连续内容，优先在合理的连续镜头内完成。切镜须有明确表达需要，运镜服务人物动作和观看重点，避免无意义切换、复杂炫技及一镜内隐藏多次切镜；不得为了减少镜头省略剧情或对白。", nil
	default:
		return "", fault.ErrInvalid
	}
}

// 报价与启动共用同一构造；仅后台可决定上限，客户端不能提交自造能力。
func (s *Service) freezeBreakdownOptions(ctx context.Context, p d.Project, scope *d.StoryboardScope, mode string) error {
	if scope.Operation != "breakdown" {
		if mode != "" {
			return fault.ErrInvalid
		}
		return nil
	}
	if _, e := breakdownInstruction(mode); e != nil {
		return e
	}
	if mode == "" {
		mode = "general"
	}
	scope.BreakdownMode = mode
	if p.ProjectSettings == nil || p.ProjectSettings.VideoModel == "" {
		return nil
	}
	options, e := s.AvailableModels(ctx)
	if e != nil {
		return e
	}
	for _, model := range options.Video {
		if model.ID != p.ProjectSettings.VideoModel {
			continue
		}
		if model.MediaProfile == nil || model.MediaProfile.Retired {
			break
		}
		longest := 0.0
		for _, seconds := range model.MediaProfile.Durations {
			if !math.IsNaN(seconds) && !math.IsInf(seconds, 0) {
				longest = max(longest, seconds)
			}
		}
		if longest <= 0 {
			break
		}
		scope.VideoConstraint = &d.StoryboardVideoConstraint{ModelID: model.ID, ModelName: model.Name, MaxDurationSeconds: longest}
		return nil
	}
	return fault.New("VIDEO_MODEL_UNAVAILABLE", "所选视频模型不可用或缺少最长时长能力，请检查模型配置")
}

func storyboardTaskInstruction(scope *d.StoryboardScope) (string, error) {
	if scope == nil {
		return "", nil
	}
	// 正文任务只消费冻结镜头；不能收到拆镜或重新选择资产的行动指令。
	switch scope.Operation {
	case "fillPrompts":
		return "\n本次仅补齐 taskScope.shotIds 中缺失或失效的 videoPrompt。场景、顺序、动作、对白、摄影、designNotes、时长及资产选择均为只读，其他镜头仅供理解承接。不得借正文修复改动输入或补写目标外镜头。只读输入本身矛盾或不可自然完成时指出具体阻断，不越权拆镜、改时长或改选择，不伪造完成。", nil
	case "regeneratePrompts":
		return "\n本次仅替换 taskScope.shotIds 明确指定镜头的 videoPrompt。沿用已保存动作、完整对白、摄影、designNotes、时长和资产选择；邻镜只作上下文。输入本身矛盾或不可自然完成时指出具体阻断，不越权拆镜、改时长或改选择，不伪造完成。", nil
	case "breakdown":
	default:
		return "", fault.ErrInvalid
	}
	mode, e := breakdownInstruction(scope.BreakdownMode)
	if e != nil {
		return "", e
	}
	text := "\n本次生成当前集完整拆解：完成场景、连续镜头、资产选择及全部视频正文；可在本集范围内修订这些内容，已有正确结果与用户内容须保留。需要拆镜时同步保存动作、完整对白、引用、正文和承接，不能仅修改秒数。breakdown 的既有 shotIds 不是禁止创建本集新镜头的白名单。"
	text += "\n本次拆解模式：" + mode + "场景数、镜头数和整集总时长由实际内容决定，不设数量目标。镜头时长根据动作、完整对白、表演停顿和摄影自主估算，durationSeconds 输出正整数秒，不把视频模型上限当作每镜默认时长；小数秒会在采用时向上取整后校验上限。"
	if v := scope.VideoConstraint; v != nil {
		text += fmt.Sprintf("本任务所选视频模型为 %s（%s），每个连续镜头最长 %g 秒；durationSeconds 须为正数且不超过该值。完整内容装不下时按叙事合理拆成相邻连续镜头，保持全部剧情、完整对白和连续性；不得仅裁剪秒数、加速台词、压缩剧情或把多次切镜藏在一个 Shot。收到 SHOT_DURATION_EXCEEDED 后须重新核对完整内容的自然完成时间，不能为了通过数值校验只缩短 durationSeconds。", v.ModelName, v.ModelID, v.MaxDurationSeconds)
	}
	text += "\n生成后和独立复核时，逐镜从原文与当前实际动作、完整对白、停顿及摄影重新估算自然完成时间，再与已保存 durationSeconds 和本任务上限（如有）比较。不要从已填秒数倒推内容可行；对白和动作只有确实可以同时发生时才重叠计算，不能假定加速台词或省略必要停顿。发现装不下，指出具体镜头和动作/对白依据，按叙事边界拆成相邻连续镜头，并同步保存各镜的动作、完整对白、视频正文及衔接；不能只改秒数或把原来的整段内容重复塞入每个新镜头。修正后读回实际结果，再核对完整剧情、对白、资产引用和前后承接，保留其他正确内容。独立复核须自行作出这个判断，不能把已填秒数、工具保存成功或生成者的估计当作证明。"
	return text, nil
}

// 只规范模型提交的镜头时长，不改原答、正文或用户编辑；取整后仍经过领域上限校验。
func roundModelShotDurations(operations []d.StoryboardOperation) error {
	for i := range operations {
		op := &operations[i]
		var raw *json.RawMessage
		switch op.Op {
		case "createShot":
			raw = &op.Value
		case "updateShot":
			raw = &op.Patch
		default:
			continue
		}
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(*raw, &fields); err != nil {
			return err
		}
		value, ok := fields["durationSeconds"]
		if !ok || string(value) == "null" {
			continue
		}
		var seconds float64
		if err := json.Unmarshal(value, &seconds); err != nil {
			return err
		}
		if seconds == math.Ceil(seconds) {
			continue
		}
		fields["durationSeconds"] = mustJSON(math.Ceil(seconds))
		*raw = mustJSON(fields)
	}
	return nil
}
