package api

import (
	"fmt"
	"io/ioutil"
	"net/http"

	"github.com/NebulousLabs/Sia/encoding"
	"github.com/NebulousLabs/Sia/types"
)

// minerBlockforworkHandler handles the API call that retrieves a block for
// work.
func (srv *Server) minerBlockforworkHandler(w http.ResponseWriter, req *http.Request) {
	bfw, _, target := srv.miner.BlockForWork()
	w.Write(encoding.MarshalAll(target, bfw.Header(), bfw))
}

// minerHeaderforworkHandler handles the API call that retrieves a block header
// for work.
func (srv *Server) minerHeaderforworkHandler(w http.ResponseWriter, req *http.Request) {
	bhfw, target := srv.miner.HeaderForWork()
	w.Write(encoding.MarshalAll(target, bhfw))
}

// minerStartHandler handles the API call that starts the miner.
func (srv *Server) minerStartHandler(w http.ResponseWriter, req *http.Request) {
	// Scan for the number of threads.
	var threads int
	_, err := fmt.Sscan(req.FormValue("threads"), &threads)
	if err != nil {
		writeError(w, "Malformed number of threads", http.StatusBadRequest)
		return
	}

	srv.miner.SetThreads(threads)
	err = srv.miner.StartMining()
	if err != nil {
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeSuccess(w)
}

// minerStatusHandler handles the API call that queries the miner's status.
func (srv *Server) minerStatusHandler(w http.ResponseWriter, req *http.Request) {
	writeJSON(w, srv.miner.MinerInfo())
}

// minerStopHandler handles the API call to stop the miner.
func (srv *Server) minerStopHandler(w http.ResponseWriter, req *http.Request) {
	srv.miner.StopMining()
	writeSuccess(w)
}

// minerSubmitblockHandler handles the API call to submit a block to the miner.
func (srv *Server) minerSubmitblockHandler(w http.ResponseWriter, req *http.Request) {
	var b types.Block
	encodedBlock, err := ioutil.ReadAll(req.Body)
	if err != nil {
		writeError(w, err.Error(), http.StatusBadRequest)
		return
	}
	err = encoding.Unmarshal(encodedBlock, &b)
	if err != nil {
		writeError(w, err.Error(), http.StatusBadRequest)
		return
	}

	err = srv.miner.SubmitBlock(b)
	if err != nil {
		writeError(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeSuccess(w)
}

// minerSubmitheaderHandler handles the API call to submit a block header to the
// miner.
func (srv *Server) minerSubmitheaderHandler(w http.ResponseWriter, req *http.Request) {
	var bh types.BlockHeader
	encodedHeader, err := ioutil.ReadAll(req.Body)
	if err != nil {
		writeError(w, err.Error(), http.StatusBadRequest)
		return
	}
	err = encoding.Unmarshal(encodedHeader, &bh)
	if err != nil {
		writeError(w, err.Error(), http.StatusBadRequest)
		return
	}
	err = srv.miner.SubmitHeader(bh)
	if err != nil {
		writeError(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeSuccess(w)
}
