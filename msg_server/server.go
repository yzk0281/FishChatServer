//
// Copyright 2014 Hong Miao (miaohong@miaohong.org). All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"time"
	"flag"
	"sync"
	"encoding/json"
	"github.com/oikomi/FishChatServer/log"
	"github.com/oikomi/FishChatServer/libnet"
	"github.com/oikomi/FishChatServer/base"
	"github.com/oikomi/FishChatServer/protocol"
	"github.com/oikomi/FishChatServer/storage/redis_store"
)

func init() {
	flag.Set("alsologtostderr", "true")
	flag.Set("log_dir", "false")
}

type MsgServer struct {
	cfg               *MsgServerConfig
	sessions          base.SessionMap
	channels          base.ChannelMap
	topics            protocol.TopicMap
	server            *libnet.Server
	sessionStore      *redis_store.SessionCache
	topicStore        *redis_store.TopicStore
	offlineMsgStore   *redis_store.OfflineMsgStore
	p2pAckStatus      base.AckMap
	scanSessionMutex  sync.Mutex
	p2pAckMutex       sync.Mutex
}

func NewMsgServer(cfg *MsgServerConfig, rs *redis_store.RedisStore) *MsgServer {
	return &MsgServer {
		cfg                : cfg,
		sessions           : make(base.SessionMap),
		channels           : make(base.ChannelMap),
		topics             : make(protocol.TopicMap),
		server             : new(libnet.Server),
		sessionStore       : redis_store.NewSessionCache(rs),
		topicStore         : redis_store.NewTopicStore(rs),
		offlineMsgStore    : redis_store.NewOfflineMsgStore(rs),
		p2pAckStatus       : make(base.AckMap),
	}
}

func (self *MsgServer)createChannels() {
	log.Info("createChannels")
	for _, c := range base.ChannleList {
		channel := libnet.NewChannel(self.server.Protocol())
		self.channels[c] = base.NewChannelState(c, channel)
	}
}

func (self *MsgServer)sendMonitorData() error {
	log.Info("sendMonitorData")
	resp := protocol.NewCmdMonitor()

	// resp.SessionNum = (uint64)(len(self.sessions))
	
	// log.Info(resp)

	mb := NewMonitorBeat("monitor", self.cfg.MonitorBeatTime, 40, 10)
	
	if self.channels[protocol.SYSCTRL_MONITOR] != nil {
		for{
			resp.SessionNum = (uint64)(len(self.sessions))
	
			//log.Info(resp)
			mb.Beat(self.channels[protocol.SYSCTRL_MONITOR].Channel, resp)
		} 
		// _, err := self.channels[protocol.SYSCTRL_MONITOR].Channel.Broadcast(libnet.Json(resp))
		// if err != nil {
		// 	glog.Error(err.Error())
		// 	return err
		// }
	}

	return nil
}

func (self *MsgServer)scanDeadSession() {
	log.Info("scanDeadSession")
	timer := time.NewTicker(self.cfg.ScanDeadSessionTimeout * time.Second)
	ttl := time.After(self.cfg.Expire * time.Second)
	for {
		select {
		case <-timer.C:
			log.Info("scanDeadSession timeout")
			go func() {
				for id, s := range self.sessions {
					self.scanSessionMutex.Lock()
					//defer self.scanSessionMutex.Unlock()
					if (s.State).(*base.SessionState).Alive == false {
						log.Info("delete" + id)
						delete(self.sessions, id)
					} else {
						s.State.(*base.SessionState).Alive = false
					}
					self.scanSessionMutex.Unlock()
				}
			}()
		case <-ttl:
			break
		}
	}
}

func (self *MsgServer)parseProtocol(cmd []byte, session *libnet.Session) error {
	var c protocol.CmdSimple
	err := json.Unmarshal(cmd, &c)
	if err != nil {
		log.Error("error:", err)
		return err
	}
	
	pp := NewProtoProc(self)

	switch c.GetCmdName() {
		case protocol.SEND_PING_CMD:
			err = pp.procPing(&c, session)
			if err != nil {
				log.Error("error:", err)
				return err
			}
		case protocol.SUBSCRIBE_CHANNEL_CMD:
			pp.procSubscribeChannel(&c, session)
		case protocol.SEND_CLIENT_ID_CMD:
			err = pp.procClientID(&c, session)
			if err != nil {
				log.Error("error:", err)
				return err
			}
		case protocol.SEND_MESSAGE_P2P_CMD:
			err = pp.procSendMessageP2P(&c, session)
			if err != nil {
				log.Error("error:", err)
				return err
			}
		case protocol.ROUTE_MESSAGE_P2P_CMD:
			err = pp.procRouteMessageP2P(&c, session)
			if err != nil {
				log.Error("error:", err)
				return err
			}
		case protocol.CREATE_TOPIC_CMD:
			err = pp.procCreateTopic(&c, session)
			if err != nil {
				log.Error("error:", err)
				return err
			}
		case protocol.JOIN_TOPIC_CMD:
			err = pp.procJoinTopic(&c, session)
			if err != nil {
				log.Error("error:", err)
				return err
			}
		case protocol.SEND_MESSAGE_TOPIC_CMD:
			err = pp.procSendMessageTopic(&c, session)
			if err != nil {
				log.Error("error:", err)
				return err
			}

		// p2p ack
		case protocol.P2P_ACK_CMD:
			err = pp.procP2pAck(&c, session)
			if err != nil {
				log.Error("error:", err)
				return err
			}
		}

	return err
}
