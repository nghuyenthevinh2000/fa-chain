package keeper

import (
	"fmt"
	"strings"
	"time"

	"github.com/cosmos/cosmos-sdk/telemetry"
	sdk "github.com/cosmos/cosmos-sdk/types"

	ibctransfertypes "github.com/cosmos/ibc-go/v3/modules/apps/transfer/types"
	appparams "github.com/notional-labs/fa-chain/app/params"
	"github.com/notional-labs/fa-chain/x/feeabstraction/types"
	gammtypes "github.com/osmosis-labs/osmosis/v13/x/gamm/types"
	twapquery "github.com/osmosis-labs/osmosis/v13/x/twap/client/queryproto"
)

func (k Keeper) BeginBlocker(ctx sdk.Context) {
	defer telemetry.ModuleMeasureSince(types.ModuleName, time.Now(), telemetry.MetricKeyBeginBlocker)

	k.Logger(ctx).Info("Discovering ibc tokens from osmosis")

	// due to ibc denom hash, we can only accept denom directly from Osmosis
	// is there a way to get all assets on a channel
	k.transferKeeper.IterateDenomTraces(ctx, func(denomTrace ibctransfertypes.DenomTrace) bool {
		k.Logger(ctx).Info(fmt.Sprintf("Found token pair: (%s, %s) on channel %s",
			denomTrace.GetBaseDenom(), denomTrace.IBCDenom(), osmo_juno_channel_id))

		// if an ibc denom exists, skip
		if k.HasDenomTrack(ctx, denomTrace.GetBaseDenom()) {
			return true
		}

		// if found out that denom belongs to osmosis channel_id, register denom trace
		if strings.Contains(denomTrace.GetPath(), osmo_juno_channel_id) {
			k.Logger(ctx).Info("Registering token pair")
			k.SetDenomTrack(ctx, denomTrace.GetBaseDenom(), denomTrace.IBCDenom())
		}

		return true
	})

	// get pools information from Osmosis
	// make request for pools
	k.IterateDenomTrack(ctx, func(denomOsmo, _ string) bool {

		// check if it has pool
		if k.HasPool(ctx, denomOsmo) {
			return true
		}

		req := gammtypes.QueryPoolsWithFilterRequest{
			MinLiquidity: sdk.NewCoins(sdk.NewCoin(denomOsmo, sdk.OneInt()), sdk.NewCoin(GetIBCDenom(osmo_juno_channel_id, appparams.DefaultBondDenom).IBCDenom(), sdk.OneInt())),
			PoolType:     "Balancer",
		}

		k.Logger(ctx).Info(fmt.Sprintf("Preparing msg = %v", req))

		data, err := req.Marshal()
		if err != nil {
			k.Logger(ctx).Error("failed to marshall request", "error", err)
			return true
		}
		ttl, err := k.GetTtl(ctx)
		if err != nil {
			k.Logger(ctx).Error("failed to cast value", "error", err)
			return true
		}

		if err := k.icqKeeper.MakeRequest(ctx,
			types.ModuleName,
			ICQCallbackID_Pool,
			host_zone_chain_id,
			juno_osmo_connection_id,
			types.POOL_STORE_QUERY,
			data,
			ttl,
		); err != nil {
			k.Logger(ctx).Error("failed to make request", "error", err)
			return true
		}

		return true
	})
}

// EndBlocker of feeabstraction module
func (k Keeper) EndBlocker(ctx sdk.Context) {
	defer telemetry.ModuleMeasureSince(types.ModuleName, time.Now(), telemetry.MetricKeyEndBlocker)

	k.Logger(ctx).Info("Fetching fee rate for ibc tokens from osmosis")

	// update fee
	k.IteratePool(ctx, func(denomOsmo string, poolId uint64) bool {
		k.Logger(ctx).Info(fmt.Sprintf("Found pool: (%s, %d)", denomOsmo, poolId))

		// make request for twap
		// TODO: better handling of start time
		req := twapquery.ArithmeticTwapToNowRequest{
			PoolId:     poolId,
			BaseAsset:  denomOsmo,
			QuoteAsset: GetIBCDenom(osmo_juno_channel_id, appparams.DefaultBondDenom).IBCDenom(),
			StartTime:  time.Now().Add(-time.Second * 10),
		}
		data, err := req.Marshal()
		if err != nil {
			k.Logger(ctx).Error("failed to marshall request", "error", err)
			return true
		}
		ttl, err := k.GetTtl(ctx)
		if err != nil {
			k.Logger(ctx).Error("failed to cast value", "error", err)
			return true
		}

		if err := k.icqKeeper.MakeRequest(ctx,
			types.ModuleName,
			ICQCallbackID_FeeRate,
			host_zone_chain_id,
			juno_osmo_connection_id,
			types.TWAP_STORE_QUERY,
			data,
			ttl,
		); err != nil {
			k.Logger(ctx).Error("failed to make request", "error", err)
			return true
		}

		return true
	})
}
