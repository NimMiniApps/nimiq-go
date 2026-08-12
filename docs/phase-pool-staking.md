# Validator lifecycle fixture validation

The validator lifecycle fixtures were confirmed on 2026-08-12 against a disposable four-validator `dev-albatross` built from `core-rs-albatross` commit `b12430dd3`. The node's transaction-builder RPC output and `nimiq-go` serialization matched byte-for-byte at validity height 111.

`DeactivateValidator` was built and signed by `nimiq-go`, accepted in block 243, and executed successfully as transaction `8f9d75b743bd14c54d411643c0ee1acfabb4fb7207968b57397f21d9bb8b7941`.

`ReactivateValidator` was built and signed by `nimiq-go` after the deactivation settled, accepted in block 258, and executed successfully as transaction `e74414d65257e0a6740df80e6ac638583450e39cb42d2729384aeab6b6af5ba6`.

`RetireValidator` was built and signed by `nimiq-go` after reactivation, accepted in block 273, and executed successfully as transaction `9a79f9887a7843e751749b193bf55739422fb44c27d5f248f7817ecb4fe8c84c`. A subsequent `getValidatorByAddress` returned `retired: true`.
