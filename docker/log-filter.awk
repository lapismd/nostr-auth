BEGIN {
  print "role=" role " event=log_filter_started"
  fflush()
}

{
  line = $0
  lower = tolower(line)

  if (line ~ /response=/) {
    print "role=" role " event=nip46_response response=[REDACTED]"
  } else if (lower ~ /(private[_ -]?shard|secret[_ -]?key|nsec|oauth[_ -]?token|recovery[_ -]?material)/) {
    print "role=" role " event=sensitive_log_redacted"
  } else {
    print "role=" role " message=" line
  }
  fflush()
}

