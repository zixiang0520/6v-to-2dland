package drive

// File 是网盘文件/目录的统一结构（前端 JSON 字段与旧 2dland 版对齐）。
type File struct {
	Identity    string `json:"identity"`
	Name        string `json:"name"`
	Path        string `json:"path"`
	Dir         bool   `json:"dir"`
	Size        int64  `json:"size"`
	UpdateTs    int64  `json:"update_ts"`
	Files       int64  `json:"files"`
	Dirs        int64  `json:"dirs"`
	Direcotries int64  `json:"direcotries"` // 历史字段名（SDK 拼写），前端未强依赖
}

// UserTask 是离线任务（字段对齐原前端：identity/name/url/save_path/status/progress）。
// status：0 等待 / 2 下载中 / 3 失败 / 1000 已完成（前端沿用此枚举）。
type UserTask struct {
	Identity       string  `json:"identity"`
	Name           string  `json:"name"`
	Url            string  `json:"url"`
	SavePath       string  `json:"save_path"`
	Status         int32   `json:"status"`
	Progress       float64 `json:"progress"`
	BytesTotal     int64   `json:"bytes_total"`
	BytesProcessed int64   `json:"bytes_processed"`
	Organized      bool    `json:"organized"`
}
