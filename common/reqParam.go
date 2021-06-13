package common

// ReqParam 可选择传入的参数
type ReqParam struct {
	UserExtList []string	// 用户确认的视频后缀名支持列表
	SaveMultiSub bool		// 存储每个网站 Top1 的字幕
	DebugMode bool			// 调试标志位
	RemoteBrowserDockerURL string // rod 使用远程的浏览器
	FoundExistSubFileThanSkip bool	// 如果视频的目录下面有字幕文件了，就跳过
	UserRemoteBrowser			bool		// 是否使用远程的浏览器去爬取 subhd 的字
	RemoteBrowserDockerURL 		string		// 远程 rod-browser 的 ws 地址
	FoundExistSubFileThanSkip 	bool		// 是否跳过已经下载过 sub 的视频


	HttpProxy string		// HttpClient 相关
	UserAgent string		// HttpClient 相关
	Referer   string		// HttpClient 相关
	MediaType string		// HttpClient 相关
	Charset   string		// HttpClient 相关
	Topic	  int			// 搜索结果的时候，返回 Topic N 以内的
}
