# Minimal workspace placeholder.
# Tagged as hermes-unified:saas to match the image name hard-coded in
# savvy-manager/app/docker_manager.py, so the manager pulls this without
# any code change. Replace this Dockerfile (or re-tag) once a real Hermes
# workspace image is available.
FROM nginx:alpine

COPY index.html /usr/share/nginx/html/index.html
COPY nginx.conf /etc/nginx/conf.d/default.conf

EXPOSE 3000
