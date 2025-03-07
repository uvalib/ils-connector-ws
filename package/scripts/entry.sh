# set blank options variables
SMTP_USER_OPT=""
SMTP_PASS_OPT=""

# SMTP username
if [ -n "${V4_SMPT_USER}" ]; then
   SMTP_USER_OPT="-smtpuser ${V4_SMPT_USER}"
fi

# SMTP password
if [ -n "${V4_SMPT_PASS}" ]; then
   SMTP_PASS_OPT="-smtppass ${V4_SMPT_PASS}"
fi

# run from here
cd bin; ./ils-connector-ws \
  -userkey ${AUTH_SHARED_SECRET} \
  -jwtkey ${JWT_KEY} \
  -pda ${PDA_BASE_URL} \
  -port ${SERVICE_PORT} \
  -sirsiclient ${SIRSI_CLIENT_ID} \
  -sirsilibrary ${SIRSI_LIBRARY} \
  -sirsipass ${SIRSI_PASSWORD} \
  -sirsiscript ${SIRSI_SCRIPT_URL} \
  -sirsiurl ${SIRSI_WEB_SERVICES_BASE} \
  -sirsiuser ${SIRSI_USER} \
  -solr ${V4_SOLR_URL} \
  -core ${V4_SOLR_CORE} \
  -hsilliad ${V4_HSL_ILLIAD_URL} \
  -userinfo ${USERINFO_URL} \
  -virgo ${SEARCH_URL} \
  -smtphost ${V4_SMPT_HOST} \
  -smtpport ${V4_SMPT_PORT} \
  -smtpsender ${V4_SMPT_SENDER} \
  -cremail ${V4_CR_EMAIL} \
  -lawemail ${V4_LAW_CR_EMAIL} \
  ${SMTP_USER_OPT} \
  ${SMTP_PASS_OPT}

return $?

#
# end of file
#
