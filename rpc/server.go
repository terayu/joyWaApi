//go:build server
// +build server

package rpc

import (
	"context"
	"github.com/terayu/joyWaApi/core"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"log"
	"net"
	"strconv"
	"time"
)

type Server struct {
	api     *core.Handle // DLL Handle
	gSvr    *grpc.Server //grpc server ptr
	Network string       // Network TCP/UDP
	Address string       // Address IP
}

func (s *Server) mustEmbedUnimplementedJoyWaApiServer() {
	// This is a marker method required by gRPC
}

type ServerOption func(svr *Server)

func SetNetWork(network string) (s ServerOption) {
	return func(svr *Server) {
		svr.Network = network
	}
}
func SetAddress(address string) (s ServerOption) {
	return func(svr *Server) {
		svr.Address = address
	}
}
func NewServer(opts ...ServerOption) *Server {
	log.Printf("trace: joyWaApi:rpc:server:NewServer")
	svr := new(Server)
	svr.api = core.New()

	for i := 0; i < len(opts); i++ {
		opts[i](svr)
	}
	return svr

}

// ServerStart start joyWaApi grpc server
// this operation blocking
func (s *Server) ServerStart() error {
	log.Printf("trace: joyWaApi:rpc:server:ServerStart")
	ln, err := net.Listen(s.Network, s.Address)
	if err != nil {
		log.Printf("warning: joyWaApi:rpc:server:ServerStart:net.Listen Fail(%v)", err)
		return err
	}
	s.gSvr = grpc.NewServer()

	RegisterJoyWaApiServer(s.gSvr, s)

	if err := s.gSvr.Serve(ln); err != nil {
		log.Printf("warning: joyWaApi:rpc:server:ServerStart:grpc.Serve Fail(%v)", err)
		return err
	}
	return nil
}
func (s *Server) ServerStop() {
	// 待ち受けに失敗したときは gSvr がない
	if s.gSvr != nil {
		s.gSvr.Stop()
	}
}

// checkTagVals タグ数と値の数が一致しているか確認する
func checkTagVals(nTags, nVals int) error {
	if nTags != nVals {
		return status.Errorf(codes.InvalidArgument, "number of tags(%d) and vals(%d) mismatch", nTags, nVals)
	}
	return nil
}

// GetAPI returns the API handle
func (s *Server) GetAPI() *core.Handle {
	return s.api
}
func (s *Server) RStartServer(ctx context.Context, requestType *RequestType) (*NoReturnMessage, error) {
	log.Printf("trace: joyWaApi:rpc:server:RStartServer")
	if err := s.api.StartServer(); err != nil {
		log.Printf("warning: joyWaApi:rpc:server:RStartServer:StartServer Fail(%v)", err)
		return &NoReturnMessage{}, err
	}
	return &NoReturnMessage{}, nil
}

func (s *Server) RConnect(context.Context, *RequestType) (nr *NoReturnMessage, err error) {
	log.Printf("trace: joyWaApi:rpc:server:RConnect")
	if err := s.api.Connect(); err != nil {
		log.Printf("warning: joyWaApi:rpc:server:RConnect:Connect Fail(%v)", err)
		return &NoReturnMessage{}, err
	}
	return &NoReturnMessage{}, nil
}

func (s *Server) RDisConnect(context.Context, *RequestType) (*NoReturnMessage, error) {
	log.Printf("trace: joyWaApi:rpc:server:RDisConnect")
	if err := s.api.DisConnect(); err != nil {
		log.Printf("warning: joyWaApi:rpc:server:RDisConnect:DisConnect Fail(%v)", err)
		return &NoReturnMessage{}, err
	}
	return &NoReturnMessage{}, nil
}
func (s *Server) RDisConnectForce(context.Context, *RequestType) (*NoReturnMessage, error) {
	log.Printf("trace: joyWaApi:rpc:server:DisConnectForce")
	if err := s.api.DisConnectForce(); err != nil {
		log.Printf("warning: joyWaApi:rpc:server:RDisConnect:DisConnectForce Fail(%v)", err)
		return &NoReturnMessage{}, err
	}
	return &NoReturnMessage{}, nil
}
func (s *Server) RPing(ctx context.Context, requestType *RequestType) (*BoolMessage, error) {
	log.Printf("trace: joyWaApi:rpc:server:RPing")
	_, err := s.api.Ping()
	if err != nil {
		log.Printf("warning: joyWaApi:rpc:server:RPing:Ping Fail(%v)", err)
		return &BoolMessage{Val: false}, nil
	}
	return &BoolMessage{Val: true}, nil
}
func (s *Server) RRead(ctx context.Context, tm *TagsMessage) (ret *ValsMessage, err error) {
	log.Printf("trace: joyWaApi:rpc:server:RRead")
	ret = new(ValsMessage)
	ret.Vals = make([]string, len(tm.Tags))

	for i := 0; i < len(tm.Tags); i++ {
		v, err := s.api.ReadS(tm.Tags[i])
		if err != nil {
			log.Printf("warning: joyWaApi:rpc:server:RRead:ReadS Fail(%v)", err)
			return &ValsMessage{}, err
		}

		log.Printf("trace: joyWaApi:rpc:server:RRead tag:%v value:%v", tm.Tags[i], v)
		ret.Vals[i] = v
	}
	return ret, nil
}

func (s *Server) RReadF(ctx context.Context, tm *TagsMessage) (ret *ValsMessage, err error) {
	log.Printf("trace: joyWaApi:rpc:server:RReadF")
	ret = new(ValsMessage)
	ret.Vals = make([]string, len(tm.Tags))

	for i := 0; i < len(tm.Tags); i++ {
		v, err := s.api.ReadF(tm.Tags[i])
		if err != nil {
			log.Printf("warning: joyWaApi:rpc:server:RReadF:ReadF Fail(%v)", err)
			return &ValsMessage{}, err
		}

		log.Printf("trace: joyWaApi:rpc:server:RReadF tag:%v value:%v", tm.Tags[i], v)
		ret.Vals[i] = strconv.FormatFloat(v, 'f', 4, 64)
	}
	return ret, nil
}

func (s *Server) RWriteF(ctx context.Context, tm *TagValsMessage) (nr *NoReturnMessage, err error) {
	log.Printf("trace: joyWaApi:rpc:server:RWriteF")
	if err := checkTagVals(len(tm.Tags), len(tm.Vals)); err != nil {
		log.Printf("warning: joyWaApi:rpc:server:RWriteF:%v", err)
		return &NoReturnMessage{}, err
	}
	for i := 0; i < len(tm.Vals); i++ {
		v, err := strconv.ParseFloat(tm.Vals[i], 64)
		log.Printf("debug: RWriteF str=%v float=%v", tm.Vals, v)
		if err != nil {
			log.Printf("warning: joyWaApi:rpc:server:RWriteF:ParseFloat Fail(%v)", err)
			return &NoReturnMessage{}, err
		}
		if err := s.api.WriteFloat(tm.Tags[i], v); err != nil {
			log.Printf("warning: joyWaApi:rpc:server:RWriteF:WriteFloat Fail(%v)", err)
			return &NoReturnMessage{}, err
		}
		log.Printf("trace: joyWaApi:rpc:server:RWriteF tag:%v value:%v", tm.Tags[i], v)
	}
	return &NoReturnMessage{}, nil
}

func (s *Server) RReadB(ctx context.Context, tm *TagBitMessage) (ret *BitMessage, err error) {
	log.Printf("trace: joyWaApi:rpc:server:RReadB")
	ret = new(BitMessage)
	ret.Vals = make([]bool, len(tm.Tags))

	for i := 0; i < len(tm.Tags); i++ {
		v, err := s.api.ReadB(tm.Tags[i])
		if err != nil {
			log.Printf("warning: joyWaApi:rpc:server:RReadB:ReadB Fail(%v)", err)
			return &BitMessage{}, err
		}
		log.Printf("trace: joyWaApi:rpc:server:RReadB:ReadB tag:%v value:%v", tm.Tags[i], v)

		ret.Vals[i] = v
	}
	return ret, nil
}

func (s *Server) RWriteB(ctx context.Context, tm *TagBitMessage) (nr *NoReturnMessage, err error) {
	log.Printf("trace: joyWaApi:rpc:server:RWriteB")
	if err := checkTagVals(len(tm.Tags), len(tm.Vals)); err != nil {
		log.Printf("warning: joyWaApi:rpc:server:RWriteB:%v", err)
		return &NoReturnMessage{}, err
	}
	if len(tm.Pulse) > 0 && len(tm.Pulse) != len(tm.Tags) {
		err := status.Errorf(codes.InvalidArgument, "number of tags(%d) and pulse(%d) mismatch", len(tm.Tags), len(tm.Pulse))
		log.Printf("warning: joyWaApi:rpc:server:RWriteB:%v", err)
		return &NoReturnMessage{}, err
	}

	for i := 0; i < len(tm.Vals); i++ {
		var v float64
		if tm.Vals[i] {
			v = 1
		} else {
			v = 0
		}

		if err := s.api.WriteFloat(tm.Tags[i], v); err != nil {
			log.Printf("warning: joyWaApi:rpc:server:RWriteB:WriteFloat Fail(%v)", err)
			return &NoReturnMessage{}, err
		}

		//For Pulse
		if len(tm.Pulse) > 0 {

			log.Printf("trace: joyWaApi:rpc:server:RWrite:tag[%v] val[%v] palsu[%v]", tm.Tags[i], tm.Vals[i], tm.Pulse[i])
			if tm.Pulse[i] > 0 {
				tag := tm.Tags[i]
				pulse := tm.Pulse[i]
				go func() {
					done := time.After(time.Second * time.Duration(pulse))

					<-done
					log.Printf("trace: joyWaApi:rpc:server:RWrite:tag[%v] Pulse Off", tag)
					if err := s.api.WriteFloat(tag, 0); err != nil {
						log.Printf("warning: joyWaApi:rpc:server:RWriteB:WriteFloat Fail(%v)", err)
						return
					}
				}()
			}
		}

		log.Printf("trace: joyWaApi:rpc:server:RWriteB tag:%v value:%v", tm.Tags[i], v)
	}
	return &NoReturnMessage{}, nil
}
func (s *Server) RWriteS(ctx context.Context, tm *TagValsMessage) (*NoReturnMessage, error) {
	log.Printf("trace: joyWaApi:rpc:server:RWriteS")
	if err := checkTagVals(len(tm.Tags), len(tm.Vals)); err != nil {
		log.Printf("warning: joyWaApi:rpc:server:RWriteS:%v", err)
		return &NoReturnMessage{}, err
	}
	for i := 0; i < len(tm.Vals); i++ {
		v := tm.Vals[i]

		if err := s.api.WriteString(tm.Tags[i], v); err != nil {
			log.Printf("warning: joyWaApi:rpc:server:RWriteS:WriteString Fail(%v)", err)
			return &NoReturnMessage{}, err
		}
		log.Printf("trace: joyWaApi:rpc:server:RWriteS tag:%v value:%v", tm.Tags[i], v)
	}
	return &NoReturnMessage{}, nil
}

func (s *Server) RWrite(ctx context.Context, tm *TagValsMessage) (*NoReturnMessage, error) {
	log.Printf("trace: joyWaApi:rpc:server:RWrite")
	if err := checkTagVals(len(tm.Tags), len(tm.Vals)); err != nil {
		log.Printf("warning: joyWaApi:rpc:server:RWrite:%v", err)
		return &NoReturnMessage{}, err
	}
	for i := 0; i < len(tm.Vals); i++ {
		v := tm.Vals[i]

		if err := s.api.Write(tm.Tags[i], v); err != nil {
			log.Printf("warning: joyWaApi:rpc:server:RWrite:Write Fail(%v)", err)
			return &NoReturnMessage{}, err
		}
		log.Printf("trace: joyWaApi:rpc:server:RWrite tag:%v value:%v", tm.Tags[i], v)
	}
	return &NoReturnMessage{}, nil
}
