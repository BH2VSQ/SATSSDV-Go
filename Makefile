.PHONY: test vet check backend qt6 native-check backend-rpc-check

test:
	go test ./...

vet:
	go vet ./...

native-check:
	python3 scripts/check_native_ui.py

backend-rpc-check:
	python3 scripts/test_backend_rpc.py

check: test vet native-check backend-rpc-check

backend:
	cmake -S . -B build -DSATSSDV_BUILD_BACKEND_ONLY=ON
	cmake --build build --parallel

qt6:
	cmake -S . -B build-qt6 -DCMAKE_BUILD_TYPE=Release -DSATSSDV_WITH_MALGO=ON
	cmake --build build-qt6 --parallel
