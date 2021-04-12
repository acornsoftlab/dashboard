package termapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	log "github.com/sirupsen/logrus"

	"context"

	"encoding/base64"

	"github.com/acornsoftlab/dashboard/pkg/app"
	"github.com/acornsoftlab/dashboard/pkg/config"
	"github.com/acornsoftlab/dashboard/pkg/lang"
	"github.com/acornsoftlab/dashboard/pkg/terminal/backend/localcommand"
	cache "github.com/acornsoftlab/dashboard/pkg/terminal/cache/token"
	"github.com/acornsoftlab/dashboard/pkg/terminal/pkg/randomstring"
	"github.com/acornsoftlab/dashboard/pkg/terminal/server"
	"github.com/acornsoftlab/dashboard/pkg/terminal/utils"
	"github.com/acornsoftlab/dashboard/pkg/terminal/webtty"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/pkg/errors"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

//터미널 API요청구조
type TermRequest struct {
	Termtype  string `form:"termtype" binding:"required"`
	Cluster   string `form:"cluster"`
	Pod       string `form:"pod"`
	Container string `form:"container"`
}

var instSvr *server.Server
var counter *server.Counter

func init() {
	//서버생성용
	var err error
	instSvr, err = makeSvr()
	if err != nil {
		log.Errorf("web terminal server create error (cause=%v)", err)
	}

	// 연결 수 관리용 Counter구성
	//Todo Timeout 시간 설정
	counter = server.NewCounter(time.Duration(instSvr.Options.Timeout) * time.Second)

}

func makeSvr() (*server.Server, error) {

	// 서버 옵션을 기본 값으로 설정
	appOptions := &server.Options{}
	if err := utils.ApplyDefaultValues(appOptions); err != nil {
		return nil, err
	}

	appOptions.PermitWrite = true

	// Backend 처리 (LocalCommand 처리용) 옵션을 기본 값으로 설정
	backendOptions := &localcommand.Options{}
	if err := utils.ApplyDefaultValues(backendOptions); err != nil {
		return nil, err
	}

	//터미널 실행시 전달되는 쉘환경 구성 스크립트
	initScript := "/opt/k3webterminal/config-namespace.sh"
	initArgs := []string{}

	// LocalCommand로 사용할 옵션과 명령어, 파라미터를 기준으로 LocalCommand Factory 생성
	factory, err := localcommand.NewFactory(initScript, initArgs, backendOptions)
	if err != nil {

		return nil, err
	}

	// Session 상태 저장을 위한 Redis 사용 여부 및 Redis 서버 연결 옵션을 기본 값으로 설정
	redisOptions := &server.RedisOptions{}
	if err := utils.ApplyDefaultValues(redisOptions); err != nil {
		return nil, err
	}

	// LocalCommand Factory와 옵션들을 기준으로 구동할 서버 인스턴스 생성
	svr, err := server.New(factory, appOptions, redisOptions)
	if err != nil {
		return nil, err
	}

	return svr, nil
}

func MakeAuthToken(c *gin.Context) {
	g := app.Gin{C: c}

	// 터미널 연결 식별을 위한 일회성 랜덤 문자열 생성 및 정보 설정
	token := randomstring.Generate(20)

	//API요청 파라미터 파싱

	cluster := lang.NVL(g.C.Param("CLUSTER"), config.Value.CurrentContext)
	namespace := lang.NVL(g.C.Param("NAMESPACE"), "")
	pod := g.C.Param("POD")
	container := lang.NVL(g.C.Param("CONTAINER"), "")
	termtype := ""

	if len(g.C.Param("POD")) == 0 {
		termtype = "cluster"
	} else {
		termtype = "pod"
	}

	kubeConfig, err := GetContext(cluster)
	if err != nil {
		g.SendMessage(http.StatusInternalServerError, "Unable to find request Context")
		return
	}

	ttyParameter := cache.TtyParameter{}
	ttyParameter.Arg = append(ttyParameter.Arg, termtype)
	ttyParameter.Arg = append(ttyParameter.Arg, kubeConfig)
	ttyParameter.Arg = append(ttyParameter.Arg, namespace)
	ttyParameter.Arg = append(ttyParameter.Arg, pod)
	ttyParameter.Arg = append(ttyParameter.Arg, container)
	//ttyParameter.Arg = append(ttyParameter.Arg, namespace)

	//캐시 등록
	if err := instSvr.Cache.Add(token, &ttyParameter, cache.DefaultExpiration); err != nil {
		log.Errorf("save token and ttyParam err:%v", err)
		g.SendMessage(http.StatusInternalServerError, "save token and ttyParam err")
		return
	}

	g.Send(http.StatusOK, map[string]interface{}{
		"Success": true,
		"Token":   token,
	})

}

func GenerateHandleWS(c *gin.Context) {
	//g := app.Gin{C: c}

	ctx, cancel := context.WithCancel(c)

	go func() {
		select {
		case <-counter.Timer().C:
			cancel()
		case <-ctx.Done():
		}
	}()

	num := counter.Add(1)
	closeReason := "unknown reason"

	defer func() {
		num := counter.Done()

		log.Info(fmt.Sprintf("Connection closed: %s, reason: %s, connections: %d/%d", c.Request.RemoteAddr, closeReason, num, instSvr.Options.MaxConnection))
		if instSvr.Options.Once {
			cancel()
		}
	}()

	log.Info(fmt.Sprintf("New client connected: %s, connections: %d/%d", c.Request.RemoteAddr, num, instSvr.Options.MaxConnection))

	instSvr.Upgrader.ReadBufferSize = webtty.MaxBufferSize
	instSvr.Upgrader.WriteBufferSize = webtty.MaxBufferSize
	instSvr.Upgrader.EnableCompression = true
	conn, err := instSvr.Upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		closeReason = err.Error()
		return
	}
	defer conn.Close()
	conn.SetCompressionLevel(9)
	err = processWSConn(ctx, conn)

	switch err {
	case ctx.Err():
		closeReason = "cancelation"
	case webtty.ErrSlaveClosed:
		closeReason = instSvr.Factory.Name()
	case webtty.ErrMasterClosed:
		closeReason = "client close"
	case webtty.ErrConnectionLostPing:
		closeReason = webtty.ErrConnectionLostPing.Error()
	default:
		closeReason = fmt.Sprintf("an error: %s", err)
	}

}

// processWSConn - 터미널과 연결할 WebSocket 연결 구성
func processWSConn(ctx context.Context, conn *websocket.Conn) error {
	fmt.Println("processWSConn")
	typ, initLine, err := conn.ReadMessage()
	if err != nil {
		return errors.Wrapf(err, "failed to authenticate websocket connection")
	}
	if typ != websocket.TextMessage {
		return errors.New("failed to authenticate websocket connection: invalid message type")
	}

	//클라이언트에서 전달받은 토큰처리
	var init server.InitMessage
	err = json.Unmarshal(initLine, &init)
	if err != nil {
		return errors.Wrapf(err, "failed to authenticate websocket connection")
	}

	params := map[string][]string{}

	if len(init.AuthToken) > 0 {
		ttyParameter := instSvr.Cache.Get(init.AuthToken)
		cachedKey := init.AuthToken

		if ttyParameter != nil {
			params["arg"] = ttyParameter.Arg
			instSvr.Cache.Delete(cachedKey)
		} else {
			return errors.New("ERROR:Invalid Token")
		}
	} else {
		return errors.New("ERROR:No Token Provided")
	}

	//Backend Slave생성
	var slave server.Slave
	slave, err = instSvr.Factory.New(params)
	if err != nil {
		return errors.Wrapf(err, "failed to create backend")
	}
	defer slave.Close()

	//Webtty 설정
	opts := []webtty.Option{}

	if instSvr.Options.PermitWrite {
		opts = append(opts, webtty.WithPermitWrite())
	}
	if instSvr.Options.EnableReconnect {
		opts = append(opts, webtty.WithReconnect(instSvr.Options.ReconnectTime))
	}
	if instSvr.Options.Width > 0 {
		opts = append(opts, webtty.WithFixedColumns(instSvr.Options.Width))
	}
	if instSvr.Options.Height > 0 {
		opts = append(opts, webtty.WithFixedRows(instSvr.Options.Height))
	}
	if instSvr.Options.Preferences != nil {
		opts = append(opts, webtty.WithMasterPreferences(instSvr.Options.Preferences))
	}

	tty, err := webtty.New(&server.WsWrapper{conn}, slave, opts...)
	if err != nil {
		return errors.Wrapf(err, "failed to create webtty")
	}

	err = tty.Run(ctx)

	return err
}

//기존 client-go kubeconfig 정보사용
func GetContext(pContext string) (string, error) {
	conf := config.Value.KubeConfig

	var context *clientcmdapi.Context
	if conf.Contexts[pContext] != nil {
		context = conf.Contexts[pContext]
	} else {
		return "", errors.New("context not found")
	}

	termKubeConfig := clientcmdapi.NewConfig()
	termKubeConfig.Kind = conf.Kind
	termKubeConfig.APIVersion = conf.APIVersion

	termKubeConfig.Clusters[context.Cluster] = conf.Clusters[context.Cluster].DeepCopy()
	termKubeConfig.Contexts[pContext] = context.DeepCopy()
	termKubeConfig.CurrentContext = pContext
	termKubeConfig.AuthInfos[context.AuthInfo] = conf.AuthInfos[context.AuthInfo].DeepCopy()

	resultb, err := clientcmd.Write(*termKubeConfig)

	if err != nil {
		return "", err
	}

	encKubeConfig := base64.StdEncoding.EncodeToString(resultb)

	return encKubeConfig, nil
}
