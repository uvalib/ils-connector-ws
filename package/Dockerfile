#
# build the target application
#
FROM public.ecr.aws/docker/library/golang:1.24.4-alpine3.22 AS builder

# update the packages
RUN apk update && apk upgrade && apk add --no-cache make

WORKDIR /build
COPY go.mod go.sum Makefile ./
COPY cmd ./cmd
COPY data ./data
COPY templates ./templates
RUN make linux

#
# build the target container
#
FROM public.ecr.aws/docker/library/alpine:3.22

# update the packages
RUN apk update && apk upgrade && apk add bash tzdata ca-certificates curl && rm -rf /var/cache/apk/*

# Create the run user and group
RUN addgroup webservice && adduser webservice -G webservice -D

# set the timezone appropriatly
ENV TZ=UTC
RUN cp /usr/share/zoneinfo/${TZ} /etc/localtime && echo ${TZ} > /etc/timezone

# Specify home
ENV APP_HOME=/ils-connector-ws
WORKDIR ${APP_HOME}

# Create necessary directories
RUN mkdir -p ${APP_HOME} ${APP_HOME}/bin ${APP_HOME}/bin/data $APP_HOME/bin/templates ${APP_HOME}/scripts
RUN chown -R webservice ${APP_HOME} && chgrp -R webservice ${APP_HOME}

# port and run command
EXPOSE 8080
CMD ["scripts/entry.sh"]

# Move in necessary assets
COPY package/data/container_bash_profile /home/webservice/.profile
COPY package/scripts/entry.sh ${APP_HOME}/scripts/entry.sh
COPY data/* ${APP_HOME}/bin/data/
COPY templates/* ${APP_HOME}/bin/templates/
COPY --from=builder /build/bin/ils-connector-ws.linux ${APP_HOME}/bin/ils-connector-ws

# Ensure permissions are correct
RUN chown webservice:webservice /home/webservice/.profile ${APP_HOME}/scripts/entry.sh ${APP_HOME}/bin/ils-connector-ws ${APP_HOME}/bin/data $APP_HOME/bin/templates && chmod 755 /home/webservice/.profile ${APP_HOME}/scripts/entry.sh ${APP_HOME}/bin/ils-connector-ws ${APP_HOME}/bin/data $APP_HOME/bin/templates

# Add the build tag
ARG BUILD_TAG
RUN test -n "$BUILD_TAG" && touch ${APP_HOME}/buildtag.build-$BUILD_TAG || touch ${APP_HOME}/buildtag.build-0

# Specify the user
USER webservice

#
# end of file
#
