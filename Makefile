.PHONY: test vet check backend qt6 native-check backend-rpc-check rx-gif-check packaging-check gofmt-check quality

test:
	go test ./...

vet:
	go vet ./...

native-check:
	python3 scripts/check_native_ui.py

backend-rpc-check:
	python3 scripts/test_backend_rpc.py

rx-gif-check:
	python3 scripts/check_rx_gif_preview.py

packaging-check:
	python3 scripts/check_packaging_contract.py

gofmt-check:
	python3 scripts/check_gofmt.py

quality:
	npx --yes aislop@0.20.0 ci

check: test vet gofmt-check native-check backend-rpc-check rx-gif-check packaging-check

backend:
	cmake -S . -B build -DSATSSDV_BUILD_BACKEND_ONLY=ON
	cmake --build build --parallel

qt6:
	cmake -S . -B build-qt6 -DCMAKE_BUILD_TYPE=Release -DSATSSDV_WITH_MALGO=ON
	cmake --build build-qt6 --parallel
