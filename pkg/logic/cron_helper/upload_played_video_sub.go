package cron_helper

import (
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/allanpk716/ChineseSubFinder/pkg"

	"github.com/allanpk716/ChineseSubFinder/pkg/common"

	"github.com/allanpk716/ChineseSubFinder/internal/dao"
	"github.com/allanpk716/ChineseSubFinder/internal/models"
	"github.com/allanpk716/ChineseSubFinder/pkg/mix_media_info"
	"github.com/jinzhu/now"
)

// uploadVideoSub  上传字幕的定时器
func (ch *CronHelper) uploadVideoSub() {

	ch.uploadPlayedVideoSub()

	ch.uploadLowTrustVideoSub()
}

func (ch *CronHelper) uploadPlayedVideoSub() {

	// 找出没有上传过的字幕列表
	var notUploadedVideoSubInfos []models.VideoSubInfo
	dao.GetDb().Where("is_send = ?", false).Limit(1).Find(&notUploadedVideoSubInfos)

	if len(notUploadedVideoSubInfos) < 1 {
		ch.Logger.Debugln("No notUploadedVideoSubInfos")
		return
	}

	var imdbInfos []models.IMDBInfo
	dao.GetDb().Where("imdb_id = ?", notUploadedVideoSubInfos[0].IMDBInfoID).Find(&imdbInfos)
	if len(imdbInfos) < 1 {
		// 如果没有找到，那么就没有办法推断出 IMDB ID 的相关信息和 TMDB ID 信息，要来何用，删除即可
		ch.Logger.Infoln("No imdbInfos, will delete this VideoSubInfo,", notUploadedVideoSubInfos[0].SubName)
		dao.GetDb().Delete(&notUploadedVideoSubInfos[0])
		return
	}
	videoType := ""
	if imdbInfos[0].IsMovie == true {
		videoType = "movie"
	} else {
		videoType = "series"
	}
	var err error
	var finalQueryIMDBInfo *models.MediaInfo
	if imdbInfos[0].TmdbId == "" {

		// 需要先对这个字幕的 IMDB ID 转 TMDB ID 信息进行查询，得到 TMDB ID 和 Year (2019 2022)
		finalQueryIMDBInfo, err = mix_media_info.GetMediaInfoAndSave(ch.FileDownloader.MediaInfoDealers, &imdbInfos[0], imdbInfos[0].IMDBID, "imdb", videoType)
		if err != nil {
			ch.Logger.Errorln(errors.New("GetMediaInfoAndSave error:" + err.Error()))
			return
		}
	} else {

		var mediaInfos []models.MediaInfo
		dao.GetDb().Where("tmdb_id = ?", imdbInfos[0].TmdbId).Find(&mediaInfos)
		if len(mediaInfos) < 1 {
			finalQueryIMDBInfo, err = mix_media_info.GetMediaInfoAndSave(ch.FileDownloader.MediaInfoDealers, &imdbInfos[0], imdbInfos[0].IMDBID, "imdb", videoType)
			if err != nil {
				ch.Logger.Errorln(errors.New("GetMediaInfoAndSave error:" + err.Error()))
				return
			}
		} else {
			finalQueryIMDBInfo = &mediaInfos[0]
		}
	}
	// 在这之前，需要进行一次判断，这个字幕是否是有效的，因为可能会有是 1kb 的错误字幕
	// 如果解析这个字幕是错误的，那么也可以标记完成
	shareRootDir, err := pkg.GetShareSubRootFolder()
	if err != nil {
		ch.Logger.Errorln("GetShareSubRootFolder error:", err.Error())
		return
	}
	bok, _, err := ch.FileDownloader.SubParserHub.DetermineFileTypeFromFile(filepath.Join(shareRootDir, notUploadedVideoSubInfos[0].StoreRPath))
	if err != nil {
		notUploadedVideoSubInfos[0].IsSend = true
		dao.GetDb().Save(&notUploadedVideoSubInfos[0])
		ch.Logger.Errorln("DetermineFileTypeFromFile upload sub error, mark is send,", err.Error())
		return
	}
	if bok == false {
		notUploadedVideoSubInfos[0].IsSend = true
		dao.GetDb().Save(&notUploadedVideoSubInfos[0])
		ch.Logger.Errorln("DetermineFileTypeFromFile upload sub == false, not match any SubType, mark is send")
		return
	}

	ch.Logger.Infoln("AskFroUpload", notUploadedVideoSubInfos[0].SubName)
	// 问询这个字幕是否上传过了，如果没有就需要进入上传的队列
	askForUploadReply, err := ch.FileDownloader.MediaInfoDealers.SubtitleBestApi.AskFroUpload(
		notUploadedVideoSubInfos[0].SHA256,
		notUploadedVideoSubInfos[0].IsMovie,
		true,
		finalQueryIMDBInfo.ImdbId,
		finalQueryIMDBInfo.TmdbId,
		notUploadedVideoSubInfos[0].Season,
		notUploadedVideoSubInfos[0].Episode,
		notUploadedVideoSubInfos[0].Feature,
	)
	if err != nil {
		ch.Logger.Errorln(fmt.Errorf("AskFroUpload err: %v", err))
		return
	}
	if askForUploadReply.Status == 3 {
		// 上传过了，直接标记本地的 is_send 字段为 true
		notUploadedVideoSubInfos[0].IsSend = true
		dao.GetDb().Save(&notUploadedVideoSubInfos[0])
		ch.Logger.Infoln("Subtitle has been uploaded, so will not upload again")
		return
	} else if askForUploadReply.Status == 4 {
		// 上传队列满了，等待下次定时器触发
		ch.Logger.Infoln("Subtitle upload queue is full, will try ask upload again")
		return
	} else if askForUploadReply.Status == 2 {
		// 这个上传任务已经在队列中了，也许有其他人也需要上传这个字幕，或者本机排队的时候故障了，重启也可能遇到这个故障
		ch.Logger.Infoln("Subtitle is int the queue")
		return
	} else if askForUploadReply.Status == 1 {
		// 正确放入了队列，然后需要按规划的时间进行上传操作
		// 这里可能需要执行耗时操作来等待到安排的时间点进行字幕的上传，不能直接长时间的 Sleep 操作
		// 每次 Sleep 1s 然后就判断一次定时器是否还允许允许，如果不运行了，那么也就需要退出循环

		// 得到目标时间与当前时间的差值，单位是s
		waitTime := askForUploadReply.ScheduledUnixTime - time.Now().Unix()
		if waitTime <= 0 {
			waitTime = 5
		}
		ch.Logger.Infoln("will wait", waitTime, "s 2 upload sub 2 server")
		var sleepCounter int64
		sleepCounter = 0
		normalStatus := false
		for ch.cronHelperRunning == true {
			if sleepCounter > waitTime {
				normalStatus = true
				break
			}
			if sleepCounter%30 == 0 {
				ch.Logger.Infoln("wait 2 upload sub")
			}
			time.Sleep(1 * time.Second)
			sleepCounter++
		}
		if normalStatus == false || ch.cronHelperRunning == false {
			// 说明不是正常跳出来的，是结束定时器来执行的
			ch.Logger.Infoln("uploadVideoSub early termination")
			return
		}
		// 发送字幕

		releaseTime, err := now.Parse(finalQueryIMDBInfo.Year)
		if err != nil {
			ch.Logger.Errorln("now.Parse error:", err.Error())
			return
		}
		ch.Logger.Infoln("UploadSub", notUploadedVideoSubInfos[0].SubName)
		uploadSubReply, err := ch.FileDownloader.MediaInfoDealers.SubtitleBestApi.UploadSub(&notUploadedVideoSubInfos[0], shareRootDir, finalQueryIMDBInfo.TmdbId, strconv.Itoa(releaseTime.Year()))
		if err != nil {
			ch.Logger.Errorln("UploadSub error:", err.Error())

			if errors.Is(err, common.ErrorUpload413) == true {
				// 文件发送大小超限
				notUploadedVideoSubInfos[0].IsSend = true
				dao.GetDb().Save(&notUploadedVideoSubInfos[0])
				ch.Logger.Infoln("subtitle upload file over size limit, will not upload again")
				return
			}

			return
		}
		if uploadSubReply.Status == 1 {
			// 成功，其他情况就等待 Ask for Upload
			notUploadedVideoSubInfos[0].IsSend = true
			dao.GetDb().Save(&notUploadedVideoSubInfos[0])
			ch.Logger.Infoln("subtitle is uploaded")
			return
		} else if uploadSubReply.Status == 0 {

			// 发送失败，然后需要判断具体的错误，有一些需要直接标记已发送，跳过
			if strings.Contains(uploadSubReply.Message, "sub file sha256 not match") == true ||
				strings.Contains(uploadSubReply.Message, "determine sub file type error") == true ||
				strings.Contains(uploadSubReply.Message, "determine sub file type not match") == true ||
				strings.Contains(uploadSubReply.Message, "sub file has no chinese") == true {
				notUploadedVideoSubInfos[0].IsSend = true
				dao.GetDb().Save(&notUploadedVideoSubInfos[0])
				ch.Logger.Infoln("subtitle upload error, uploadSubReply.Status == 0, ", uploadSubReply.Message, "will not upload again")
				return
			} else {
				ch.Logger.Errorln("subtitle upload error, uploadSubReply.Status == 0, not support error:", uploadSubReply.Message)
				return
			}
		} else {
			ch.Logger.Warningln("UploadSub Message:", uploadSubReply.Message)
			return
		}

	} else {
		// 不是预期的返回值，需要报警
		ch.Logger.Errorln(fmt.Errorf("AskFroUpload Not the expected return value, Status: %d, Message: %v", askForUploadReply.Status, askForUploadReply.Message))
		return
	}
}

func (ch *CronHelper) uploadLowTrustVideoSub() {

	// 找出没有上传过的字幕列表
	var notUploadedVideoSubInfos []models.LowVideoSubInfo
	dao.GetDb().Where("is_send = ?", false).Limit(1).Find(&notUploadedVideoSubInfos)

	if len(notUploadedVideoSubInfos) < 1 {
		ch.Logger.Debugln("No notUploadedVideoSubInfos")
		return
	}

	var imdbInfos []models.IMDBInfo
	dao.GetDb().Where("imdb_id = ?", notUploadedVideoSubInfos[0].IMDBID).Find(&imdbInfos)
	if len(imdbInfos) < 1 {
		// 如果没有找到，那么就没有办法推断出 IMDB ID 的相关信息和 TMDB ID 信息，要来何用，删除即可
		ch.Logger.Infoln("No imdbInfos, will delete this VideoSubInfo,", notUploadedVideoSubInfos[0].SubName)
		dao.GetDb().Delete(&notUploadedVideoSubInfos[0])
		return
	}

	videoType := ""
	if notUploadedVideoSubInfos[0].Season == 0 && notUploadedVideoSubInfos[0].Episode == 0 {
		videoType = "movie"
	} else if (notUploadedVideoSubInfos[0].Season == 0 && notUploadedVideoSubInfos[0].Episode != 0) || (notUploadedVideoSubInfos[0].Season != 0 && notUploadedVideoSubInfos[0].Episode == 0) {
		ch.Logger.Errorln(notUploadedVideoSubInfos[0].SubName, "has Season or Episode error")
		ch.Logger.Errorln("season - episode", notUploadedVideoSubInfos[0].Season, notUploadedVideoSubInfos[0].Episode)
		// 成功，其他情况就等待 Ask for Upload
		notUploadedVideoSubInfos[0].IsSend = true
		dao.GetDb().Save(&notUploadedVideoSubInfos[0])
		ch.Logger.Infoln("subtitle will skip upload")
		return
	} else {
		videoType = "series"
	}

	var err error
	var finalQueryIMDBInfo *models.MediaInfo
	if imdbInfos[0].TmdbId == "" {

		// 需要先对这个字幕的 IMDB ID 转 TMDB ID 信息进行查询，得到 TMDB ID 和 Year (2019 2022)
		finalQueryIMDBInfo, err = mix_media_info.GetMediaInfoAndSave(ch.FileDownloader.MediaInfoDealers, &imdbInfos[0], imdbInfos[0].IMDBID, "imdb", videoType)
		if err != nil {
			ch.Logger.Errorln(errors.New("GetMediaInfoAndSave error:" + err.Error()))
			return
		}
	} else {

		var mediaInfos []models.MediaInfo
		dao.GetDb().Where("tmdb_id = ?", imdbInfos[0].TmdbId).Find(&mediaInfos)
		if len(mediaInfos) < 1 {
			finalQueryIMDBInfo, err = mix_media_info.GetMediaInfoAndSave(ch.FileDownloader.MediaInfoDealers, &imdbInfos[0], imdbInfos[0].IMDBID, "imdb", videoType)
			if err != nil {
				ch.Logger.Errorln(errors.New("GetMediaInfoAndSave error:" + err.Error()))
				return
			}
		} else {
			finalQueryIMDBInfo = &mediaInfos[0]
		}
	}
	// 在这之前，需要进行一次判断，这个字幕是否是有效的，因为可能会有是 1kb 的错误字幕
	// 如果解析这个字幕是错误的，那么也可以标记完成
	shareRootDir, err := pkg.GetShareSubRootFolder()
	if err != nil {
		ch.Logger.Errorln("GetShareSubRootFolder error:", err.Error())
		return
	}
	bok, _, err := ch.FileDownloader.SubParserHub.DetermineFileTypeFromFile(filepath.Join(shareRootDir, notUploadedVideoSubInfos[0].StoreRPath))
	if err != nil {
		notUploadedVideoSubInfos[0].IsSend = true
		dao.GetDb().Save(&notUploadedVideoSubInfos[0])
		ch.Logger.Errorln("DetermineFileTypeFromFile upload sub error, mark is send,", err.Error())
		return
	}
	if bok == false {
		notUploadedVideoSubInfos[0].IsSend = true
		dao.GetDb().Save(&notUploadedVideoSubInfos[0])
		ch.Logger.Errorln("DetermineFileTypeFromFile upload sub == false, not match any SubType, mark is send")
		return
	}

	ch.Logger.Infoln("AskFroUpload", notUploadedVideoSubInfos[0].SubName)
	// 问询这个字幕是否上传过了，如果没有就需要进入上传的队列
	askForUploadReply, err := ch.FileDownloader.MediaInfoDealers.SubtitleBestApi.AskFroUpload(
		notUploadedVideoSubInfos[0].SHA256,
		notUploadedVideoSubInfos[0].IsMovie,
		false,
		"",
		"",
		0,
		0,
		notUploadedVideoSubInfos[0].Feature,
	)
	if err != nil {
		ch.Logger.Errorln(fmt.Errorf("AskFroUpload err: %v", err))
		return
	}
	if askForUploadReply.Status == 3 {
		// 上传过了，直接标记本地的 is_send 字段为 true
		notUploadedVideoSubInfos[0].IsSend = true
		dao.GetDb().Save(&notUploadedVideoSubInfos[0])
		ch.Logger.Infoln("Subtitle has been uploaded, so will not upload again")
		return
	} else if askForUploadReply.Status == 4 {
		// 上传队列满了，等待下次定时器触发
		ch.Logger.Infoln("Subtitle upload queue is full, will try ask upload again")
		return
	} else if askForUploadReply.Status == 2 {
		// 这个上传任务已经在队列中了，也许有其他人也需要上传这个字幕，或者本机排队的时候故障了，重启也可能遇到这个故障
		ch.Logger.Infoln("Subtitle is int the queue")
		return
	} else if askForUploadReply.Status == 1 {
		// 正确放入了队列，然后需要按规划的时间进行上传操作
		// 这里可能需要执行耗时操作来等待到安排的时间点进行字幕的上传，不能直接长时间的 Sleep 操作
		// 每次 Sleep 1s 然后就判断一次定时器是否还允许允许，如果不运行了，那么也就需要退出循环

		// 得到目标时间与当前时间的差值，单位是s
		waitTime := askForUploadReply.ScheduledUnixTime - time.Now().Unix()
		if waitTime <= 0 {
			waitTime = 5
		}
		ch.Logger.Infoln("will wait", waitTime, "s 2 upload sub 2 server")
		var sleepCounter int64
		sleepCounter = 0
		normalStatus := false
		for ch.cronHelperRunning == true {
			if sleepCounter > waitTime {
				normalStatus = true
				break
			}
			if sleepCounter%30 == 0 {
				ch.Logger.Infoln("wait 2 upload sub")
			}
			time.Sleep(1 * time.Second)
			sleepCounter++
		}
		if normalStatus == false || ch.cronHelperRunning == false {
			// 说明不是正常跳出来的，是结束定时器来执行的
			ch.Logger.Infoln("uploadVideoSub early termination")
			return
		}
		// 发送字幕

		releaseTime, err := now.Parse(finalQueryIMDBInfo.Year)
		if err != nil {
			ch.Logger.Errorln("now.Parse error:", err.Error())
			return
		}
		ch.Logger.Infoln("UploadSub", notUploadedVideoSubInfos[0].SubName)
		uploadSubReply, err := ch.FileDownloader.MediaInfoDealers.SubtitleBestApi.UploadLowTrustSub(&notUploadedVideoSubInfos[0], shareRootDir, finalQueryIMDBInfo.TmdbId, strconv.Itoa(releaseTime.Year()), "")
		if err != nil {
			ch.Logger.Errorln("UploadLowTrustSub error:", err.Error())

			if errors.Is(err, common.ErrorUpload413) == true {
				// 文件发送大小超限
				notUploadedVideoSubInfos[0].IsSend = true
				dao.GetDb().Save(&notUploadedVideoSubInfos[0])
				ch.Logger.Infoln("subtitle upload file over size limit, will not upload again")
				return
			}

			return
		}
		if uploadSubReply.Status == 1 {
			// 成功，其他情况就等待 Ask for Upload
			notUploadedVideoSubInfos[0].IsSend = true
			dao.GetDb().Save(&notUploadedVideoSubInfos[0])
			ch.Logger.Infoln("subtitle is uploaded")
			return
		} else if uploadSubReply.Status == 0 {

			// 发送失败，然后需要判断具体的错误，有一些需要直接标记已发送，跳过
			if strings.Contains(uploadSubReply.Message, "sub file sha256 not match") == true ||
				strings.Contains(uploadSubReply.Message, "determine sub file type error") == true ||
				strings.Contains(uploadSubReply.Message, "determine sub file type not match") == true ||
				strings.Contains(uploadSubReply.Message, "sub file has no chinese") == true {
				notUploadedVideoSubInfos[0].IsSend = true
				dao.GetDb().Save(&notUploadedVideoSubInfos[0])
				ch.Logger.Infoln("subtitle upload error, uploadSubReply.Status == 0, Message:", uploadSubReply.Message, "will not upload again")
				return
			} else {
				ch.Logger.Errorln("subtitle upload error, uploadSubReply.Status == 0, not support error:", uploadSubReply.Message)
				return
			}
		} else {
			ch.Logger.Warningln("UploadSub Message:", uploadSubReply.Message)
			return
		}

	} else {
		// 不是预期的返回值，需要报警
		ch.Logger.Errorln(fmt.Errorf("AskFroUpload Not the expected return value, Status: %d, Message: %v", askForUploadReply.Status, askForUploadReply.Message))
		return
	}
}
