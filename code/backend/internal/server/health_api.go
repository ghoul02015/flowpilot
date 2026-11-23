package server

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/ghoul02015/flowpilot/internal/database"
	"github.com/ghoul02015/flowpilot/internal/health"
	"github.com/ghoul02015/flowpilot/internal/instance"
)

// instanceHealth 三层状态＋最近事件。
func (h *Handler) instanceHealth(w http.ResponseWriter, r *http.Request) {
	if h.instances == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "实例管理未启用"})
		return
	}
	target, err := h.instances.Get(r.Context(), r.PathValue("name"))
	if err != nil {
		h.writeInstanceError(w, err, "读取实例失败")
		return
	}
	view := health.InstanceHealth{Container: target.Status, App: health.StateNotRunning, LatenciesMS: []int{}}
	if h.healthChecker != nil {
		view = h.healthChecker.HealthOf(target)
	}
	recent := []database.InstanceEvent{}
	if h.database != nil {
		eventKey := target.UUID
		if eventKey == "" {
			eventKey = target.Name
		}
		recent = h.database.ListEvents(eventKey, 3)
	}
	writeJSON(w, http.StatusOK, map[string]any{"health": view, "recentEvents": recent})
}

// instanceEvents 事件时间线。
func (h *Handler) instanceEvents(w http.ResponseWriter, r *http.Request) {
	if h.database == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "数据底座未启用"})
		return
	}
	target, err := h.instances.Get(r.Context(), r.PathValue("name"))
	if err != nil {
		h.writeInstanceError(w, err, "读取实例失败")
		return
	}
	limit := intQuery(r, "limit", 50)
	// 旧实例无 UUID：事件以名称为关联键。
	eventKey := target.UUID
	if eventKey == "" {
		eventKey = target.Name
	}
	writeJSON(w, http.StatusOK, map[string]any{"events": h.database.ListEvents(eventKey, limit)})
}

// rebuildPlan 重建差异预览：对比期望配置（DB spec_json）与当前容器标签。
func (h *Handler) rebuildPlan(w http.ResponseWriter, r *http.Request) {
	if h.instances == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "实例管理未启用"})
		return
	}
	target, err := h.instances.Get(r.Context(), r.PathValue("name"))
	if err != nil {
		h.writeInstanceError(w, err, "读取实例失败")
		return
	}
	type planField struct {
		Field    string `json:"field"`
		Current  string `json:"current"`
		Previous string `json:"previous"`
		Changed  bool   `json:"changed"`
	}
	fields := []planField{}
	spec := h.specOf(target)
	if spec != nil {
		compare := [][3]string{
			{"镜像", target.Image, spec.Image},
			{"GPU", strconv.Itoa(target.GPU), strconv.Itoa(spec.GPU)},
			{"访问端口", strconv.Itoa(target.Port), strconv.Itoa(spec.Port)},
			{"自动启动", strconv.FormatBool(target.AutoStart), strconv.FormatBool(spec.AutoStart)},
		}
		for _, row := range compare {
			fields = append(fields, planField{Field: row[0], Current: row[1], Previous: row[2], Changed: row[1] != row[2]})
		}
	} else {
		fields = append(fields, planField{Field: "镜像", Current: target.Image, Previous: "（无期望配置记录）", Changed: false})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"name":          target.Name,
		"uuid":          target.UUID,
		"workspaceKept": target.UUID != "",
		"interrupt":     target.Status == "running",
		"fields":        fields,
		"dataNote":      "重建后容器重建、模型链接重建；实例数据目录" + map[bool]string{true: "沿用（含输出与工作流）", false: "为旧共享挂载，重建后转为独立目录"}[target.UUID != ""],
	})
}

// specOf 从数据底座读取期望配置（instance_configs.spec_json）。
func (h *Handler) specOf(target instance.Instance) *instance.Spec {
	if h.database == nil {
		return nil
	}
	raw, ok := h.database.LoadSpecRaw(target.Name)
	if !ok {
		return nil
	}
	spec := &instance.Spec{}
	if err := json.Unmarshal([]byte(raw), spec); err != nil {
		return nil
	}
	return spec
}

// queueSummary 停止/重启前读取队列，返回警告文案（无任务为空）。
func (h *Handler) queueSummary(r *http.Request, target instance.Instance) string {
	if target.Status != "running" {
		return ""
	}
	base := "http://" + h.comfyGateway + ":" + strconv.Itoa(target.Port)
	if body, err := h.comfyGet(base + "/queue"); err == nil {
		var queue struct {
			QueueRunning []json.RawMessage `json:"queue_running"`
			QueuePending []json.RawMessage `json:"queue_pending"`
		}
		if json.Unmarshal(body, &queue) == nil && (len(queue.QueueRunning) > 0 || len(queue.QueuePending) > 0) {
			return "队列中有 " + strconv.Itoa(len(queue.QueueRunning)) + " 个运行中、 " + strconv.Itoa(len(queue.QueuePending)) + " 个排队任务，操作将中断生成"
		}
	}
	return ""
}

// recordActionEvent 操作成功后写事件并同步检查器状态。
// RecordEvent 对旧实例（无 UUID）自动以名称为关联键。
func (h *Handler) recordActionEvent(r *http.Request, target instance.Instance, action string) {
	if h.database != nil {
		kind := map[string]string{"start": "started", "stop": "stopped", "restart": "restarted"}[action]
		if kind != "" {
			h.database.RecordEvent(target.UUID, target.Name, kind, "", 0)
		}
	}
	if h.healthChecker != nil {
		switch action {
		case "start", "restart":
			h.healthChecker.MarkStarted(target.Name)
		case "stop":
			h.healthChecker.MarkStopped(target.Name)
		}
	}
}
