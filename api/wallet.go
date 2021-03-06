package api

import (
	"fmt"
	"net/http"

	"github.com/NebulousLabs/Sia/consensus"
)

// walletAddressHandler handles the API request for a new address.
func (srv *Server) walletAddressHandler(w http.ResponseWriter, req *http.Request) {
	coinAddress, _, err := srv.wallet.CoinAddress()
	if err != nil {
		writeError(w, "Failed to get a coin address", http.StatusInternalServerError)
		return
	}

	// Since coinAddress is not a struct, we define one here so that writeJSON
	// writes an object instead of a bare value. In addition, we transmit the
	// coinAddress as a hex-encoded string rather than a byte array.
	writeJSON(w, struct {
		Address string
	}{fmt.Sprintf("%x", coinAddress)})
}

// walletSendHandler handles the API call to send coins to another address.
func (srv *Server) walletSendHandler(w http.ResponseWriter, req *http.Request) {
	// Scan the inputs.
	var amount consensus.Currency
	var dest consensus.UnlockHash
	_, err := fmt.Sscan(req.FormValue("amount"), &amount)
	if err != nil {
		writeError(w, "Malformed amount", http.StatusBadRequest)
		return
	}

	// Parse the string into an address.
	var destAddressBytes []byte
	_, err = fmt.Sscanf(req.FormValue("destination"), "%x", &destAddressBytes)
	if err != nil {
		writeError(w, "Malformed coin address", http.StatusBadRequest)
		return
	}
	copy(dest[:], destAddressBytes)

	// Spend the coins.
	_, err = srv.wallet.SpendCoins(amount, dest)
	if err != nil {
		writeError(w, "Failed to create transaction: "+err.Error(), http.StatusInternalServerError)
		return
	}

	writeSuccess(w)
}

// walletStatusHandler handles the API call querying the status of the wallet.
func (srv *Server) walletStatusHandler(w http.ResponseWriter, req *http.Request) {
	writeJSON(w, srv.wallet.Info())
}
