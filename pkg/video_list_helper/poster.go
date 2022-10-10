package video_list_helper

import (
	"path/filepath"

	"github.com/allanpk716/ChineseSubFinder/pkg/my_util"
)

// GetMoviePoster 获取电影的海报，如果为空就是没有找到
func (v *VideoListHelper) GetMoviePoster(movieFPath string) string {
	/*
		ext 只考虑 jpg, png, bmp 三种格式
		参考 TMM 的设置
		1. poster.ext
		2. movie.ext
		3. folder.ext
		4. <movie filename>-poster.ext
		5. <movie filename>.ext
		6. cover.ext
	*/
	for _, ext := range extList {

		movieRootDir := filepath.Dir(movieFPath)
		movieName := filepath.Base(movieFPath)
		// 1. poster.ext
		posterFPath := filepath.Join(movieRootDir, "poster"+ext)
		if my_util.IsFile(posterFPath) {
			return posterFPath
		}
		// 2. movie.ext
		posterFPath = filepath.Join(movieRootDir, "movie"+ext)
		if my_util.IsFile(posterFPath) {
			return posterFPath
		}
		// 3. folder.ext
		posterFPath = filepath.Join(movieRootDir, "folder"+ext)
		if my_util.IsFile(posterFPath) {
			return posterFPath
		}
		// 4. <movie filename>-poster.ext
		posterFPath = filepath.Join(movieRootDir, movieName+"-poster"+ext)
		if my_util.IsFile(posterFPath) {
			return posterFPath
		}
		// 5. <movie filename>.ext
		posterFPath = filepath.Join(movieRootDir, movieName+ext)
		if my_util.IsFile(posterFPath) {
			return posterFPath
		}
		// 6. cover.ext
		posterFPath = filepath.Join(movieRootDir, "cover"+ext)
		if my_util.IsFile(posterFPath) {
			return posterFPath
		}
	}
	return ""
}

// GetSeriesPoster 获取电视剧的海报，如果为空就是没有找到
func (v *VideoListHelper) GetSeriesPoster(seriesDir string) string {
	/*
		参考 TMM 的设置
		连续剧的
		1. poster.ext
		2. folder.ext
		Season的
		1. seasonXX-poster.ext
		2. <season folder>/seasonXX.ext
		3. <season folder>/folder.ext
	*/
	// 获取主封面
	for _, ext := range extList {
		// 1. poster.ext
		posterFPath := filepath.Join(seriesDir, "poster"+ext)
		if my_util.IsFile(posterFPath) {
			return posterFPath
		}
		// 2. folder.ext
		posterFPath = filepath.Join(seriesDir, "folder"+ext)
		if my_util.IsFile(posterFPath) {
			return posterFPath
		}
	}
	//files, err := filepath.Glob(filepath.Join(seriesDir, "season*-poster.jpg"))
	//if err != nil {
	//	return nil, err
	//}
	//println(files)
	// 获取每一季的封面
	//dirEntry, err := os.ReadDir(seriesDir)
	//if err != nil {
	//	return nil, err
	//}
	//for _, fi := range dirEntry {
	//	lowerName := strings.ToLower(fi.Name())
	//
	//}

	return ""
}

var (
	extList = []string{".jpg", ".png", ".bmp"}
)

type SeriesPosterInfo struct {
	SeriesPoster    string
	SeasonPosterMap map[int]string
}