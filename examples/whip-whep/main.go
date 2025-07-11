// SPDX-FileCopyrightText: 2023 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

//go:build !js
// +build !js

// whip-whep demonstrates how to use the WHIP/WHEP specifications to exchange SPD descriptions
// and stream media to a WebRTC client in the browser or OBS.
package main

import (
	"fmt"
	"io"
	"net/http"

	"github.com/pion/interceptor"
	"github.com/pion/interceptor/pkg/intervalpli"
	"github.com/pion/interceptor/pkg/packetdump"
	"github.com/pion/interceptor/pkg/report"
	"github.com/pion/rtp"
	"github.com/pion/webrtc/v4"
)

// nolint: gochecknoglobals
var (
	videoTrack *webrtc.TrackLocalStaticRTP

	peerConnectionConfiguration = webrtc.Configuration{
		ICEServers: []webrtc.ICEServer{
			{
				URLs: []string{"stun:stun.l.google.com:19302"},
			},
		},
	}
)

// nolint:gocognit
func main() {
	// Everything below is the Pion WebRTC API! Thanks for using it ❤️.
	var err error
	if videoTrack, err = webrtc.NewTrackLocalStaticRTP(webrtc.RTPCodecCapability{
		MimeType: webrtc.MimeTypeH264,
	}, "video", "pion"); err != nil {
		panic(err)
	}

	http.Handle("/", http.FileServer(http.Dir(".")))
	http.HandleFunc("/whep", whepHandler)
	http.HandleFunc("/whip", whipHandler)

	fmt.Println("Open http://localhost:8080 to access this demo")
	panic(http.ListenAndServe(":8080", nil)) // nolint: gosec
}

func whipHandler(res http.ResponseWriter, req *http.Request) { // nolint: cyclop
	// Read the offer from HTTP Request
	offer, err := io.ReadAll(req.Body)
	if err != nil {
		panic(err)
	}

	// Create a MediaEngine object to configure the supported codec
	mediaEngine := &webrtc.MediaEngine{}

	// Setup the codecs you want to use.
	// We'll only use H264 but you can also define your own
	if err = mediaEngine.RegisterCodec(webrtc.RTPCodecParameters{
		RTPCodecCapability: webrtc.RTPCodecCapability{
			MimeType: webrtc.MimeTypeH264, ClockRate: 90000, Channels: 0, SDPFmtpLine: "", RTCPFeedback: nil,
		},
		PayloadType: 96,
	}, webrtc.RTPCodecTypeVideo); err != nil {
		panic(err)
	}

	// Create a InterceptorRegistry. This is the user configurable RTP/RTCP Pipeline.
	// This provides NACKs, RTCP Reports and other features. If you use `webrtc.NewPeerConnection`
	// this is enabled by default. If you are manually managing You MUST create a InterceptorRegistry
	// for each PeerConnection.
	interceptorRegistry := &interceptor.Registry{}

	// Register a intervalpli factory
	// This interceptor sends a PLI every 3 seconds. A PLI causes a video keyframe to be generated by the sender.
	// This makes our video seekable and more error resilent, but at a cost of lower picture quality and higher bitrates
	// A real world application should process incoming RTCP packets from viewers and forward them to senders
	intervalPliFactory, err := intervalpli.NewReceiverInterceptor()
	if err != nil {
		panic(err)
	}
	interceptorRegistry.Add(intervalPliFactory)

	// Use the default set of Interceptors
	if err = webrtc.RegisterDefaultInterceptors(mediaEngine, interceptorRegistry); err != nil {
		panic(err)
	}

	// Create the API object with the MediaEngine
	api := webrtc.NewAPI(webrtc.WithMediaEngine(mediaEngine), webrtc.WithInterceptorRegistry(interceptorRegistry))

	// Create a new RTCPeerConnection
	peerConnection, err := api.NewPeerConnection(peerConnectionConfiguration)
	if err != nil {
		panic(err)
	}

	// Allow us to receive 1 video trac
	if _, err = peerConnection.AddTransceiverFromKind(webrtc.RTPCodecTypeVideo); err != nil {
		panic(err)
	}

	// Set a handler for when a new remote track starts, this handler saves buffers to disk as
	// an ivf file, since we could have multiple video tracks we provide a counter.
	// In your application this is where you would handle/process video
	peerConnection.OnTrack(func(track *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
		go func() {
			for {
				_, _, rtcpErr := receiver.ReadRTCP()
				if rtcpErr != nil {
					panic(rtcpErr)
				}
			}
		}()
		for {
			pkt, _, err := track.ReadRTP()
			if err != nil {
				panic(err)
			}
			if err = videoTrack.WriteRTP(pkt); err != nil {
				panic(err)
			}
		}
	})
	// Send answer via HTTP Response
	writeAnswer(res, peerConnection, offer, "/whip")
}

func whepHandler(res http.ResponseWriter, req *http.Request) {
	// Read the offer from HTTP Request
	offer, err := io.ReadAll(req.Body)
	if err != nil {
		panic(err)
	}

	interceptorRegistry := &interceptor.Registry{}
	packetDump, err := packetdump.NewSenderInterceptor(
		// filter out all RTP packets, only RTCP packets will be logged
		packetdump.RTPFilter(func(_ *rtp.Packet) bool {
			return false
		}),
	)
	if err != nil {
		panic(err)
	}
	interceptorRegistry.Add(packetDump)
	senderInterceptor, err := report.NewSenderInterceptor()
	if err != nil {
		panic(err)
	}
	interceptorRegistry.Add(senderInterceptor)

	api := webrtc.NewAPI(webrtc.WithInterceptorRegistry(interceptorRegistry))

	// Create a new RTCPeerConnection
	peerConnection, err := api.NewPeerConnection(peerConnectionConfiguration)
	if err != nil {
		panic(err)
	}

	// Add Video Track that is being written to from WHIP Session
	rtpSender, err := peerConnection.AddTrack(videoTrack)
	if err != nil {
		panic(err)
	}

	// Read incoming RTCP packets
	// Before these packets are returned they are processed by interceptors. For things
	// like NACK this needs to be called.
	go func() {
		rtcpBuf := make([]byte, 1500)
		for {
			if _, _, rtcpErr := rtpSender.Read(rtcpBuf); rtcpErr != nil {
				return
			}
		}
	}()

	// Send answer via HTTP Response
	writeAnswer(res, peerConnection, offer, "/whep")
}

func writeAnswer(res http.ResponseWriter, peerConnection *webrtc.PeerConnection, offer []byte, path string) {
	// Set the handler for ICE connection state
	// This will notify you when the peer has connected/disconnected
	peerConnection.OnICEConnectionStateChange(func(connectionState webrtc.ICEConnectionState) {
		fmt.Printf("ICE Connection State has changed: %s\n", connectionState.String())

		if connectionState == webrtc.ICEConnectionStateFailed {
			_ = peerConnection.Close()
		}
	})

	if err := peerConnection.SetRemoteDescription(webrtc.SessionDescription{
		Type: webrtc.SDPTypeOffer, SDP: string(offer),
	}); err != nil {
		panic(err)
	}

	// Create channel that is blocked until ICE Gathering is complete
	gatherComplete := webrtc.GatheringCompletePromise(peerConnection)

	// Create answer
	answer, err := peerConnection.CreateAnswer(nil)
	if err != nil {
		panic(err)
	} else if err = peerConnection.SetLocalDescription(answer); err != nil {
		panic(err)
	}

	// Block until ICE Gathering is complete, disabling trickle ICE
	// we do this because we only can exchange one signaling message
	// in a production application you should exchange ICE Candidates via OnICECandidate
	<-gatherComplete

	// WHIP+WHEP expects a Location header and a HTTP Status Code of 201
	res.Header().Add("Location", path)
	res.WriteHeader(http.StatusCreated)

	// Write Answer with Candidates as HTTP Response
	fmt.Fprint(res, peerConnection.LocalDescription().SDP) //nolint: errcheck
}
