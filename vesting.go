package nimiq

import "fmt"

// Vesting creation payload sizes from core-rs CreationTransactionData{8,24,32}.
// The numeric suffixes are the size of the fields after the 20-byte owner.
const (
	vestingDataShort  = AddressLen + 8          // owner + time_step
	vestingDataMedium = AddressLen + 8 + 8 + 8  // + start_time + step_amount
	vestingDataFull   = AddressLen + 8 + 8 + 8 + 8
)

// VestingCreation is the payload of a vesting contract creation.
//
// The SDK parses these but does not create them. Short encodings omit amounts
// (and sometimes start time); those fields are zero here and must be taken from
// the funding transaction's value when the application needs them.
type VestingCreation struct {
	Owner       Address
	StartTime   uint64 // milliseconds, like HTLCCreation.Timeout
	TimeStep    uint64
	StepAmount  Luna
	TotalAmount Luna
}

// ParseVestingCreation decodes the recipient data of a vesting creation
// transaction.
//
// Mirrors CreationTransactionData in
// primitives/transaction/src/account/vesting_contract.rs. Length-dependent:
// owner+step (28), owner+start+step+stepAmount (44), or all five fields (52).
func ParseVestingCreation(data []byte) (*VestingCreation, error) {
	d := &decoder{buf: data}
	ownerRaw, err := d.raw(AddressLen)
	if err != nil {
		return nil, fmt.Errorf("nimiq: vesting owner: %w", err)
	}
	owner, _ := AddressFromBytes(ownerRaw)
	v := &VestingCreation{Owner: owner}

	switch len(data) {
	case vestingDataShort:
		if v.TimeStep, err = d.u64be(); err != nil {
			return nil, fmt.Errorf("nimiq: vesting time step: %w", err)
		}
	case vestingDataMedium:
		if v.StartTime, err = d.u64be(); err != nil {
			return nil, fmt.Errorf("nimiq: vesting start time: %w", err)
		}
		if v.TimeStep, err = d.u64be(); err != nil {
			return nil, fmt.Errorf("nimiq: vesting time step: %w", err)
		}
		var step uint64
		if step, err = d.u64be(); err != nil {
			return nil, fmt.Errorf("nimiq: vesting step amount: %w", err)
		}
		v.StepAmount = Luna(step)
	case vestingDataFull:
		if v.StartTime, err = d.u64be(); err != nil {
			return nil, fmt.Errorf("nimiq: vesting start time: %w", err)
		}
		if v.TimeStep, err = d.u64be(); err != nil {
			return nil, fmt.Errorf("nimiq: vesting time step: %w", err)
		}
		var step, total uint64
		if step, err = d.u64be(); err != nil {
			return nil, fmt.Errorf("nimiq: vesting step amount: %w", err)
		}
		if total, err = d.u64be(); err != nil {
			return nil, fmt.Errorf("nimiq: vesting total amount: %w", err)
		}
		v.StepAmount = Luna(step)
		v.TotalAmount = Luna(total)
	default:
		return nil, fmt.Errorf("nimiq: vesting creation data: unexpected length %d", len(data))
	}
	if err := d.end(); err != nil {
		return nil, fmt.Errorf("nimiq: vesting creation data: %w", err)
	}
	return v, nil
}
