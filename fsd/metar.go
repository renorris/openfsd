package fsd

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
)

type metarService struct {
	numWorkers    int
	httpClient    *http.Client
	metarRequests chan metarRequest
}

type metarRequest struct {
	client   *Client
	icaoCode string
}

func newMetarService(numWorkers int) *metarService {
	return &metarService{
		numWorkers:    numWorkers,
		httpClient:    &http.Client{},
		metarRequests: make(chan metarRequest, 128),
	}
}

func (s *metarService) run(ctx context.Context) {
	for range s.numWorkers {
		go s.worker(ctx)
	}
}

func (s *metarService) worker(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case req := <-s.metarRequests:
			s.handleMetarRequest(&req)
		}
	}
}

func (s *metarService) handleMetarRequest(req *metarRequest) {
	url := buildMetarRequestURL(req.icaoCode)
	res, err := s.httpClient.Get(url)
	if err != nil {
		sendMetarServiceError(req)
		return
	}
	if res.StatusCode != http.StatusOK {
		sendMetarServiceError(req)
		return
	}

	bufBytes := make([]byte, 512)
	buf := bytes.NewBuffer(bufBytes)
	if _, err = io.Copy(buf, res.Body); err != nil {
		sendMetarServiceError(req)
		return
	}

	resBody := buf.Bytes()

	if bytes.Count(resBody, []byte("\n")) != 2 {
		fmt.Println("NOAA METAR response was invalid")
		sendMetarServiceError(req)
		return
	}

	// First line is timestamp
	resBody = resBody[bytes.IndexByte(resBody, '\n')+1:]

	// Second line is METAR and ends with \n
	resBody = resBody[:bytes.IndexByte(resBody, '\n')+1]

	packet := buildMetarResponsePacket(req.client.callsign, resBody)
	req.client.send(packet)
}

func buildMetarResponsePacket(callsign string, metar []byte) string {
	packet := strings.Builder{}
	packet.WriteString("$ARSERVER:")
	packet.WriteString(callsign)
	packet.WriteString(":METAR:")
	packet.Write(metar)
	packet.WriteString("\r\n")
	return packet.String()
}

func buildMetarRequestURL(icaoCode string) string {
	url := strings.Builder{}
	url.WriteString("https://tgftp.nws.noaa.gov/data/observations/metar/stations/")
	url.WriteString(icaoCode)
	url.WriteString(".TXT")
	return url.String()
}

func sendMetarServiceError(req *metarRequest) {
	req.client.sendError(NoWeatherProfileError, metarServiceErrString(req.icaoCode))
}

func metarServiceErrString(icaoCode string) string {
	msg := strings.Builder{}
	msg.WriteString("Error fetching METAR for ")
	msg.WriteString(icaoCode)

	return msg.String()
}

// fetchAndSendMetar fetches a METAR observation for a given ICAO code and sends it to the client once received.
// This function returns immediately once the request has been queued.
func (s *metarService) fetchAndSendMetar(ctx context.Context, client *Client, icaoCode string) {
	select {
	case <-ctx.Done():
	case s.metarRequests <- metarRequest{client: client, icaoCode: icaoCode}:
	}
}
