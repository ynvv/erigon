package clcore

import (
	"context"
	"fmt"
	"io"
	"net/http"

	"github.com/ledgerwatch/erigon-lib/kv"
	"github.com/ledgerwatch/erigon/cl/cltypes"
	cldb "github.com/ledgerwatch/erigon/cmd/erigon-cl/cl-core/cl-db"
	"github.com/ledgerwatch/log/v3"
)

func RetrieveBeaconState(ctx context.Context, uri string) (*cltypes.BeaconState, error) {
	log.Info("[Checkpoint Sync] Requesting beacon state", "uri", uri)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, uri, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Accept", "application/octet-stream")
	if err != nil {
		return nil, fmt.Errorf("checkpoint sync failed %s", err)
	}
	r, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() {
		err = r.Body.Close()
	}()
	if r.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("checkpoint sync failed, bad status code %d", r.StatusCode)
	}
	marshaled, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, fmt.Errorf("checkpoint sync failed %s", err)
	}
	beaconState := &cltypes.BeaconState{}
	err = beaconState.UnmarshalSSZ(marshaled)
	if err != nil {
		return nil, fmt.Errorf("checkpoint sync failed %s", err)
	}
	return beaconState, nil
}

func RetrieveTrustedRoot(tx kv.Tx, ctx context.Context, uri string) ([32]byte, error) {
	var update *cltypes.LightClientFinalityUpdate
	var err error
	if update, err = cldb.ReadLightClientFinalityUpdate(tx); err != nil {
		return [32]byte{}, err
	}
	if update != nil {
		return update.FinalizedHeader.HashTreeRoot()
	}

	bs, err := RetrieveBeaconState(ctx, uri)
	if err != nil {
		return [32]byte{}, err
	}
	return bs.FinalizedCheckpoint.Root, nil
}
