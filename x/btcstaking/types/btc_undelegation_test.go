package types_test

import (
	"math/rand"
	"testing"

	sdkmath "cosmossdk.io/math"
	asig "github.com/babylonchain/babylon/crypto/schnorr-adaptor-signature"
	btctest "github.com/babylonchain/babylon/testutil/bitcoin"
	"github.com/babylonchain/babylon/testutil/datagen"
	bbn "github.com/babylonchain/babylon/types"
	"github.com/babylonchain/babylon/x/btcstaking/types"
	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/btcsuite/btcd/chaincfg"
	"github.com/btcsuite/btcd/wire"
	"github.com/stretchr/testify/require"
)

func FuzzBTCUndelegation_SlashingTx(f *testing.F) {
	datagen.AddRandomSeedsToFuzzer(f, 10)

	f.Fuzz(func(t *testing.T, seed int64) {
		r := rand.New(rand.NewSource(seed))
		net := &chaincfg.SimNetParams

		delSK, _, err := datagen.GenRandomBTCKeyPair(r)
		require.NoError(t, err)

		valSK, valPK, err := datagen.GenRandomBTCKeyPair(r)
		require.NoError(t, err)
		valPKList := []*btcec.PublicKey{valPK}

		covenantSK, covenantPK, err := datagen.GenRandomBTCKeyPair(r)
		require.NoError(t, err)
		covPKList := []*btcec.PublicKey{covenantPK}

		stakingTimeBlocks := uint16(5)
		stakingValue := int64(2 * 10e8)
		slashingAddress, err := datagen.GenRandomBTCAddress(r, &chaincfg.SimNetParams)
		require.NoError(t, err)
		changeAddress, err := datagen.GenRandomBTCAddress(r, net)
		require.NoError(t, err)

		slashingRate := sdkmath.LegacyNewDecWithPrec(int64(datagen.RandomInt(r, 41)+10), 2)

		// construct the BTC delegation with everything
		btcDel, err := datagen.GenRandomBTCDelegation(
			r,
			t,
			bbn.NewBIP340PKsFromBTCPKs(valPKList),
			delSK,
			[]*btcec.PrivateKey{covenantSK},
			1,
			slashingAddress.EncodeAddress(),
			changeAddress.EncodeAddress(),
			1000,
			uint64(1000+stakingTimeBlocks),
			uint64(stakingValue),
			slashingRate,
		)
		require.NoError(t, err)

		stakingTxHash := btcDel.MustGetStakingTxHash()
		unbondingTime := uint16(100) + 1
		unbondingValue := stakingValue - 1000

		testInfo := datagen.GenBTCUnbondingSlashingInfo(
			r,
			t,
			net,
			delSK,
			valPKList,
			covPKList,
			1,
			wire.NewOutPoint(&stakingTxHash, 0),
			unbondingTime,
			unbondingValue,
			slashingAddress.EncodeAddress(),
			changeAddress.EncodeAddress(),
			slashingRate,
		)
		require.NoError(t, err)

		unbondingTxBytes, err := bbn.SerializeBTCTx(testInfo.UnbondingTx)
		require.NoError(t, err)

		// spend info of the unbonding slashing tx
		unbondingSlashingSpendInfo, err := testInfo.UnbondingInfo.SlashingPathSpendInfo()
		require.NoError(t, err)

		// delegator signs the unbonding slashing tx
		delSig, err := testInfo.SlashingTx.Sign(
			testInfo.UnbondingTx,
			0,
			unbondingSlashingSpendInfo.GetPkScriptPath(),
			delSK,
		)
		require.NoError(t, err)
		// covenant signs (using adaptor signature) the slashing tx
		encKey, err := asig.NewEncryptionKeyFromBTCPK(valPK)
		require.NoError(t, err)
		covenantSig, err := testInfo.SlashingTx.EncSign(testInfo.UnbondingTx, 0, unbondingSlashingSpendInfo.GetPkScriptPath(), covenantSK, encKey)
		require.NoError(t, err)
		covenantSigs := &types.CovenantAdaptorSignatures{
			CovPk:       bbn.NewBIP340PubKeyFromBTCPK(covenantPK),
			AdaptorSigs: [][]byte{covenantSig.MustMarshal()},
		}

		btcDel.BtcUndelegation = &types.BTCUndelegation{
			UnbondingTx:              unbondingTxBytes,
			UnbondingTime:            100 + 1,
			SlashingTx:               testInfo.SlashingTx,
			DelegatorSlashingSig:     delSig,
			CovenantSlashingSigs:     []*types.CovenantAdaptorSignatures{covenantSigs},
			CovenantUnbondingSigList: nil, // not relevant here
		}

		bsParams := &types.Params{
			CovenantPks:    bbn.NewBIP340PKsFromBTCPKs(covPKList),
			CovenantQuorum: 1,
		}
		btcNet := &chaincfg.SimNetParams

		// build slashing tx with witness for spending the unbonding tx
		unbondingSlashingTxWithWitness, err := btcDel.BuildUnbondingSlashingTxWithWitness(bsParams, btcNet, valSK)
		require.NoError(t, err)

		// assert the execution
		btctest.AssertSlashingTxExecution(t, testInfo.UnbondingInfo.UnbondingOutput, unbondingSlashingTxWithWitness)
	})
}
