#!/bin/sh
# Regenerates internal/actor/lifted.go: the town's enemy routines lifted from AMPROG.OBJ.
cd "$(dirname "$0")/.."
.venv/bin/python tools/lift.py assets/AMPROG.OBJ -o internal/actor/lifted.go \
  --local 16994-169ec --local 17008-17126 --local 175ee-17754 --local 1890e-189a0 \
  --local 18ad6-18c60 --local 18e8a-19092 --local 1966e-1972e --local 199a0-19c2a --local 174f0-175ee --local 17912-1794c --local 18582-18700 --local 178bc-17912 --local 17812-17850 --local 18698-1890e --local 1b960-1b9a2 --local 1b74a-1bc18 \
  17008:foddera 1767c:yellow 175ee:bird 1890e:rotobase 18ad6:mine 18b3e:train \
  18e8a:medtank 19bd2:camogun 16994:mill 1966e:popup 199a0:proxmine 19ad0:flame \
  178bc:player_death 18592:fire 1858a:fire_ahead 18582:fire_slow 174f0:fire_missiles \
  18698:token 18820:shield_capsule 188b2:shield_child 1b960:token_drop 17812:smart_bomb 1b74a:goose
gofmt -w internal/actor/lifted.go
